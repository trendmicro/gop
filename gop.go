package gop

import (
    "github.com/gorilla/mux"
    "github.com/gorilla/context"
    "github.com/jbert/timber"

    "fmt"
    "net"
    "time"
    "runtime"
    "net/http"
    "strings"

    "os/user"
    "syscall"
    "strconv"
)

// Stuff we include in both App and Req, for convenience
type common struct {
    timber.Logger
    Cfg             *Config
    Stats           StatsdClient
}

// Represents a managed gop app. Returned by init.
// Embeds logging, provides .Cfg for configuration access.
type App struct {
    common

    AppName         string
    ProjectName     string
    GorillaRouter   *mux.Router
    listener        net.Listener
    wantReq         chan *wantReq
    doneReq         chan *Req
    getReqs         chan chan *Req
    startTime       time.Time
    currentReqs     int
    totalReqs       int
    doingGraceful   bool
}

// Used for RPC to get a new request
type wantReq struct {
    r       *http.Request
    reply   chan *Req
}

// Per request struct. has convenience references to functionality
// in the app singleton. Passed into the request handler.
type Req struct {
    common

    id              int
    startTime       time.Time
    app             *App
    r               *http.Request
}

// The function signature your http handlers need.
type HandlerFunc func(g *Req, w http.ResponseWriter, r *http.Request)

// Set up the application. Reads config. Panic if runtime environment is deficient.
func Init(projectName, appName string) *App {
    app := &App{
        AppName:        appName,
        ProjectName:    projectName,
        GorillaRouter:  mux.NewRouter(),
        wantReq:        make(chan *wantReq),
        doneReq:        make(chan *Req),
        getReqs:        make(chan chan *Req),
        startTime:      time.Now(),
    }

    app.loadAppConfigFile()

    app.setUserAndGroup()

    app.initLogging()

    maxProcs, _ := app.Cfg.GetInt("gop", "maxprocs", 4 * runtime.NumCPU())
    app.Debug("Seting maxprocs to %d\n", maxProcs)
    runtime.GOMAXPROCS(maxProcs)

    app.initStatsd()

    app.goAgainSetup()

    app.registerGopHandlers()

    go app.watchdog()

    go app.requestMaker()

    return app
}

func (a *App) setUserAndGroup() {
    // We do not have logging set up yet. We just panic() on error.

    desiredUserName, _ := a.Cfg.Get("gop", "user", a.AppName)
    desiredUser, err := user.Lookup(desiredUserName)
    // NickG would prefer we die screaming instead of running on as root
    if err != nil {
        panic(fmt.Sprintf("Can't find user [%s] - please set config correctly and/or create user", desiredUserName))
    }

    currentUser, err := user.Current()
    if err != nil {
        panic(fmt.Sprintf("Can't find current user: %s", err.Error()))
    }
    if currentUser.Uid != desiredUser.Uid {
        numericId, err := strconv.Atoi(desiredUser.Uid)
        if err != nil {
            panic(fmt.Sprintf("Can't interpret [%s] as a numeric user id [following lookup of usernmae %s]", desiredUser.Uid, desiredUserName))
        }
        err = syscall.Setuid(numericId)
        if err != nil {
            panic(fmt.Sprintf("Can't setuid to [%s]: %s", desiredUser.Uid, err.Error()))
        }
    }

// Can't log at this stage :-/
//    a.Info("Running as user %s (%d)", desiredUserName, desiredUser.Uid)
}

// Hands out 'request' objects
func (a *App) requestMaker() {
    nextReqId := 0
    openReqs := make(map[int] *Req)
    for {
        select {
            case wantReq := <- a.wantReq: {
                req := Req{
                    common: common{
                        Logger:     a.Logger,
                        Cfg:        a.Cfg,
                        Stats:      a.Stats,
                    },

                    id:         nextReqId,
                    app:        a,
                    startTime:  time.Now(),
                    r:          wantReq.r,
                }
                openReqs[req.id] = &req
                nextReqId++
                a.Stats.Inc("http_reqs", 1)
                a.Stats.Inc("current_http_reqs", 1)
                a.totalReqs++
                a.currentReqs++
                wantReq.reply <- &req
            }
            case doneReq := <- a.doneReq: {
                _, found := openReqs[doneReq.id]
                if !found {
                    a.Error("BUG! Unknown request id [%d] being retired")
                } else {
                    a.Stats.Dec("current_http_reqs", 1)
                    doneReq.finished()
                    a.currentReqs--
                    delete (openReqs, doneReq.id)
                }
            }
            case reply := <- a.getReqs: {
                go func() {
                    for _, req := range openReqs {
                        reply <- req
                    }
                    close(reply)
                }()
            }
        }
    }
}

// Ask requestMaker for a request
func (a *App) getReq(r *http.Request) *Req {
    reply := make(chan *Req)
    a.wantReq <- &wantReq{r: r, reply: reply}
    return <- reply
}

func (g *Req) finished() {
    context.Clear(g.r)      // Cleanup  gorilla stash

    reqDuration := time.Since(g.startTime)    
    slowReqSecs, _ := g.Cfg.GetFloat32("gop", "slow_req_secs", 10)
    if reqDuration.Seconds() > float64(slowReqSecs) {
        g.Error("Slow request [%s] took %s", g.r.URL, reqDuration)
    } else {
        g.Info("Request took %s", reqDuration)
    }

    // Tidy up request finalistion (requestMaker, req.finish() method, app.requestFinished())
    restartReqs, _ := g.Cfg.GetInt("gop", "max_requests", 0)
    if restartReqs > 0 && g.app.totalReqs > restartReqs {
        g.Error("Graceful restart after max_requests: %d", restartReqs)
        g.app.StartGracefulRestart("Max requests reached")
    }
}

func (a *App) initLogging() {

    logPattern, _ := a.Cfg.Get("gop", "log_pattern", "[%D %T] [%L] %S %M")


    // If set, hack all logging to stdout for dev
    forceStdout, _:= a.Cfg.GetBool("gop", "stdout_only_logging", false)
    configLogger := timber.ConfigLogger{
        LogWriter:  new(timber.ConsoleWriter),
        Level:      timber.INFO,
        Formatter:  timber.NewPatFormatter(logPattern),
    }

    if !forceStdout {
        defaultLogDir, _ := a.Cfg.Get("gop", "log_dir", "/var/log")
        defaultLogFname := defaultLogDir + "/" + a.ProjectName + "/" + a.AppName + ".log"
        logFname, _ := a.Cfg.Get("gop", "log_file", defaultLogFname)

        newWriter, err := timber.NewFileWriter(logFname)
        if err != nil {
            panic(fmt.Sprintf("Can't open log file: %s", err))
        }
        configLogger.LogWriter = newWriter
    }

    logLevelStr, _ := a.Cfg.Get("gop", "log_level", "INFO")
    logLevelStr = strings.ToUpper(logLevelStr)
    for logLevel, levelStr := range timber.LongLevelStrings {
        if logLevelStr == levelStr {
            configLogger.Level = timber.Level(logLevel)
            break
        }
    }

    l := timber.NewTimber()
    l.AddLogger(configLogger)
    a.Logger = l
}

func (a *App) watchdog() {
    repeat, _ := a.Cfg.GetInt("gop", "watchdog_secs", 300)
    ticker := time.Tick(time.Second * time.Duration(repeat))

    for {
        sysMemBytesLimit, _ := a.Cfg.GetInt64("gop", "sysmem_bytes_limit", 0)
        sysMemBytes := int64(getSysMemBytesUsed())  // Only supports 2^63 byte systems
        a.Info("TICK: %d bytes %d current %d total", getSysMemBytesUsed, a.currentReqs, a.totalReqs)

        if sysMemBytesLimit > 0 && sysMemBytes >= sysMemBytesLimit {
            a.Error("MEM LIMIT REACHED [%d >= %d] - starting graceful restart", sysMemBytes, sysMemBytesLimit)
            a.StartGracefulRestart("Memory limit reached")
        }

        restartAfterSecs, _ := a.Cfg.GetFloat32("gop", "restart_after_secs", 0)
        appRunTime := time.Since(a.startTime).Seconds()
        if restartAfterSecs > 0 && appRunTime > float64(restartAfterSecs) {
            a.Error("TIME LIMIT REACHED [%f >= %f] - starting graceful restart", appRunTime, restartAfterSecs)
            a.StartGracefulRestart("Run time limit reached")
        }
        <- ticker
    }
}

type responseWriter struct {
    http.ResponseWriter
    app *App
    code int
    notedCode bool
}

// Satisfy the interface
func (w *responseWriter) Header() http.Header {
    return w.ResponseWriter.Header()
}

func (w *responseWriter) Write(buf []byte) (int, error) {
    w.noteCode()
    return w.ResponseWriter.Write(buf)
}

func (w *responseWriter) WriteHeader(code int) {
    w.code = code
    w.noteCode()
    w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) noteCode() {
    if !w.notedCode {
        statsKey := fmt.Sprintf("http_status.%d", w.code)
        w.app.Stats.Inc(statsKey, 1)
        w.notedCode = true
    }
}

func (a *App) WrapHandler(h HandlerFunc) http.HandlerFunc {
    // Wrap the handler, so we can do before/after logic
    f := func(w http.ResponseWriter, r *http.Request) {
        gopRequest := a.getReq(r)
        defer func() {
            a.doneReq <- gopRequest
        }()
        gopWriter := responseWriter{code: 200, app: a, ResponseWriter: w}
        // Pass in the gop, for logging, cfg etc
        h(gopRequest, &gopWriter, r)
    }

    return http.HandlerFunc(f)
}

// Register an http handler managed by gop.
// We use Gorilla muxxer, since it is back-compatible and nice to use :-)
func (a *App) HandleFunc(u string, h HandlerFunc) {
    gopHandler := a.WrapHandler(h)

    a.GorillaRouter.HandleFunc(u, gopHandler)
}

func (a *App) Run() {
    listenAddr, _ := a.Cfg.Get("gop", "listen_addr", ":http")
    listenNet, _ := a.Cfg.Get("gop", "listen_net", "tcp")
    a.goAgainListenAndServe(listenNet, listenAddr)
}

func (a *App) Serve(l net.Listener) {
    http.Serve(l, a.GorillaRouter)
}

func getSysMemBytesUsed() uint64 {
    var memStats runtime.MemStats
    runtime.ReadMemStats(&memStats)
    // There's lots of stuff in here.
    return memStats.Sys
}
