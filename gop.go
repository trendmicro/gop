package gop

import (
    "github.com/gorilla/mux"
    "github.com/jbert/timber"

    "fmt"
    "net"
    "time"
    "runtime"
    "net/http"
    "strings"
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
    currentReqs     int
    totalReqs       int
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
    }

    app.loadAppConfigFile()

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
                a.totalReqs++
                a.currentReqs++
                wantReq.reply <- &req
            }
            case doneReq := <- a.doneReq: {
                _, found := openReqs[doneReq.id]
                if !found {
                    a.Error("BUG! Unknown request id [%d] being retired")
                } else {
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
    duration := time.Now().Sub(g.startTime)
    g.Info("Request took %s", duration)
}

func (a *App) initLogging() {
    
    defaultLogDir, _ := a.Cfg.Get("gop", "log_dir", "/var/log")
    defaultLogFname := defaultLogDir + "/" + a.ProjectName + "/" + a.AppName + ".log"
    logFname, _ := a.Cfg.Get("gop", "log_file", defaultLogFname)

    writer, err := timber.NewFileWriter(logFname)
    if err != nil {
        panic(fmt.Sprintf("Can't open log file: %s", err))
    }
    configLogger := timber.ConfigLogger{
        LogWriter:  writer,
        Level:      timber.INFO,
        Formatter:  timber.NewPatFormatter("[%D %T] [%L] %S %M"),
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
        sysMem := getMemUse()
        a.Info("TICK: %d bytes %d current %d total", sysMem, a.currentReqs, a.totalReqs)
        <- ticker
    }
}

func (a *App) WrapHandler(h HandlerFunc) http.HandlerFunc {
    // Wrap the handler, so we can do before/after logic
    f := func(w http.ResponseWriter, r *http.Request) {
        gopRequest := a.getReq(r)
        defer func() {
            a.doneReq <- gopRequest
        }()
        // Pass in the gop, for logging, cfg etc
        h(gopRequest, w, r)
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


func getMemUse() uint64 {
    var memStats runtime.MemStats
    runtime.ReadMemStats(&memStats)
    // There's lots of stuff in here.
    return memStats.Sys
}
