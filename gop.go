package gop

import (
	"encoding/json"
	"github.com/gorilla/context"
	"github.com/gorilla/mux"
	"github.com/gorilla/schema"
	"github.com/jbert/timber"

	"fmt"
	"log"
	"net"
	"net/http"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// Stuff we include in both App and Req, for convenience
type common struct {
	timber.Logger
	Cfg     Config
	Stats   StatsdClient
	Decoder *schema.Decoder
}

// Represents a managed gop app. Returned by init.
// Embeds logging, provides .Cfg for configuration access.
type App struct {
	common

	AppName       string
	ProjectName   string
	GorillaRouter *mux.Router
	listener      net.Listener
	wantReq       chan *wantReq
	doneReq       chan *Req
	getReqs       chan chan *Req
	startTime     time.Time
	currentReqs   int
	totalReqs     int
	doingGraceful bool
}

// Used for RPC to get a new request
type wantReq struct {
	r     *http.Request
	reply chan *Req
}

// Per request struct. has convenience references to functionality
// in the app singleton. Passed into the request handler.
type Req struct {
	common

	id           int
	startTime    time.Time
	app          *App
	r            *http.Request
	RealRemoteIP string
	IsHTTPS      bool
}

// The function signature your http handlers need.
type HandlerFunc func(g *Req, w http.ResponseWriter, r *http.Request)

// Set up the application. Reads config. Panic if runtime environment is deficient.
func Init(projectName, appName string) *App {
	app := &App{
		common: common{
			Decoder: schema.NewDecoder(),
		},
		AppName:       appName,
		ProjectName:   projectName,
		GorillaRouter: mux.NewRouter(),
		wantReq:       make(chan *wantReq),
		doneReq:       make(chan *Req),
		getReqs:       make(chan chan *Req),
		startTime:     time.Now(),
	}

	app.loadAppConfigFile()

	// Linux setuid() doesn't work with threaded procs :-O
	// and the go runtime threads before we can get going.
	//
	// http://homepage.ntlworld.com/jonathan.deboynepollard/FGA/linux-thread-problems.html
	//    app.setUserAndGroup()

	app.initLogging()

	maxProcs, _ := app.Cfg.GetInt("gop", "maxprocs", 4*runtime.NumCPU())
	app.Debug("Setting maxprocs to %d\n", maxProcs)
	runtime.GOMAXPROCS(maxProcs)

	app.initStatsd()

	return app
}

// Clean shutdown
func (a *App) Finish() {
	// Start a log flush
	timber.Close()
}

//
// Commented out - not reliable - see comment at call site
// func (a *App) setUserAndGroup() {
//     // We do not have logging set up yet. We just panic() on error.
//
//     desiredUserName, _ := a.Cfg.Get("gop", "user", "")
//     // Usernames it's ok to run as, in order of preference
//     possibleUserNames := []string{desiredUserName, a.AppName, a.ProjectName}
//
//     doneIt := false
//     for _, desiredUserName := range possibleUserNames {
//         if runAsUserName(desiredUserName) {
//             doneIt = true
//             break
//         }
//     }
//
//     if !doneIt {
//         panic(fmt.Sprintf("Can't run as any of these users: %v, please set config and/or create user", possibleUserNames))
//     }
//
// // Can't log at this stage :-/
// //    a.Info("Running as user %s (%d)", desiredUserName, desiredUser.Uid)
// }

func (a *App) setProcessGroupForNelly() {
	// Nelly knows our pid and will check that there is always at
	// least one process in the process group with the same id as our pid
	mypid := syscall.Getpid()
	err := syscall.Setpgid(mypid, mypid)
	if err != nil {
		panic(fmt.Sprintf("Failed to setprgp]: %s\n", mypid, mypid, err.Error()))
	}
}

// Hands out 'request' objects
func (a *App) requestMaker() {
	nextReqId := 0
	openReqs := make(map[int]*Req)
	for {
		select {
		case wantReq := <-a.wantReq:
			{
				realRemoteIP := wantReq.r.RemoteAddr
				isHTTPS := false
				useXF, _ := a.Cfg.GetBool("gop", "use_xf_headers", false)
				if useXF {
					xff := wantReq.r.Header.Get("X-Forwarded-For")
					if xff != "" {
						ips := strings.Split(xff, ",")
						for i, ip := range ips {
							ips[i] = strings.TrimSpace(ip)
						}
						// The only trustworthy component is the *last* (and only if we
						// are behind nginx or other proxy which is stripping x-f-f from incoming requests)
						realRemoteIP = ips[len(ips)-1]
					}
					xfp := wantReq.r.Header.Get("X-Forwarded-Proto")
					isHTTPS = strings.ToLower(xfp) == "https"
				}
				req := Req{
					common: common{
						Logger:  a.Logger,
						Cfg:     a.Cfg,
						Stats:   a.Stats,
						Decoder: a.Decoder,
					},

					id:           nextReqId,
					app:          a,
					startTime:    time.Now(),
					r:            wantReq.r,
					RealRemoteIP: realRemoteIP,
					IsHTTPS:      isHTTPS,
				}
				openReqs[req.id] = &req
				nextReqId++
				a.Stats.Inc("http_reqs", 1)
				a.Stats.Inc("current_http_reqs", 1)
				a.totalReqs++
				a.currentReqs++
				wantReq.reply <- &req
			}
		case doneReq := <-a.doneReq:
			{
				_, found := openReqs[doneReq.id]
				if !found {
					a.Error("BUG! Unknown request id [%d] being retired")
				} else {
					a.Stats.Dec("current_http_reqs", 1)
					doneReq.finished()
					a.currentReqs--
					delete(openReqs, doneReq.id)
				}
			}
		case reply := <-a.getReqs:
			{
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
	return <-reply
}

func (g *Req) finished() {
	context.Clear(g.r) // Cleanup  gorilla stash

	reqDuration := time.Since(g.startTime)
	slowReqSecs, _ := g.Cfg.GetFloat32("gop", "slow_req_secs", 10)
	if reqDuration.Seconds() > float64(slowReqSecs) {
		g.Error("Slow request [%s] took %s", g.r.URL, reqDuration)
	} else {
		g.Debug("Request took %s", reqDuration)
	}

	// Tidy up request finalistion (requestMaker, req.finish() method, app.requestFinished())
	restartReqs, _ := g.Cfg.GetInt("gop", "max_requests", 0)
	if restartReqs > 0 && g.app.totalReqs > restartReqs {
		g.Error("Graceful restart after max_requests: %d", restartReqs)
		g.app.StartGracefulRestart("Max requests reached")
	}

	gcEveryReqs, _ := g.Cfg.GetInt("gop", "gc_requests", 0)
	if gcEveryReqs > 0 && g.app.totalReqs%gcEveryReqs == 0 {
		g.Info("Forcing GC after %d reqs", g.app.totalReqs)
		runtime.GC()
	}
}

func (g *Req) SendJson(w http.ResponseWriter, what string, v interface{}) {
	json, err := json.Marshal(v)
	if err != nil {
		g.Error("Failed to encode %s as json: %s", what, err.Error())
		http.Error(w, "Failed to encode json: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(json)
	w.Write([]byte("\n"))
}

func (a *App) initLogging() {

	defaultLogPattern := "[%D %T] [%L] %M"
	filenamesByDefault, _ := a.Cfg.GetBool("gop", "log_filename", false)
	if filenamesByDefault {
		defaultLogPattern = "[%D %T] [%L] %S %M"
	}
	logPattern, _ := a.Cfg.Get("gop", "log_pattern", defaultLogPattern)

	// If set, hack all logging to stdout for dev
	forceStdout, _ := a.Cfg.GetBool("gop", "stdout_only_logging", false)
	configLogger := timber.ConfigLogger{
		LogWriter: new(timber.ConsoleWriter),
		Level:     timber.INFO,
		Formatter: timber.NewPatFormatter(logPattern),
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

	// *Don't* create a NewTImber here. Logs are only flushed on Close() and if we
	// have more than one timber, it's easy to only Close() one of them...
	l := timber.Global
	l.AddLogger(configLogger)
	a.Logger = l

	// Set up the default go logger to go here too, so 3rd party
	// module logging plays nicely
	log.SetFlags(0)
	log.SetOutput(l)
}

func (a *App) watchdog() {
	repeat, _ := a.Cfg.GetInt("gop", "watchdog_secs", 300)
	ticker := time.Tick(time.Second * time.Duration(repeat))

	for {
		sysMemBytesLimit, _ := a.Cfg.GetInt64("gop", "sysmem_bytes_limit", 0)
		allocMemBytesLimit, _ := a.Cfg.GetInt64("gop", "allocmem_bytes_limit", 0)
		numFDsLimit, _ := a.Cfg.GetInt64("gop", "numfds_limit", 0)
		numGorosLimit, _ := a.Cfg.GetInt64("gop", "numgoros_limit", 0)

		sysMemBytes, allocMemBytes := getMemInfo()
		numFDs, err := fdsInUse()
		numGoros := int64(runtime.NumGoroutine())
		if err != nil {
			a.Error("Failed to get number of fds in use: %s", err.Error())
			// Continue without
		}

		a.Info("TICK: sys=%d,alloc=%d,fds=%d,current_req=%d,total_req=%d,goros=%d",
			sysMemBytes,
			allocMemBytes,
			numFDs,
			a.currentReqs,
			a.totalReqs,
			numGoros)
		a.Stats.Gauge("mem.sys", sysMemBytes)
		a.Stats.Gauge("mem.alloc", allocMemBytes)
		a.Stats.Gauge("numfds", numFDs)
		a.Stats.Gauge("numgoro", numGoros)

		if sysMemBytesLimit > 0 && sysMemBytes >= sysMemBytesLimit {
			a.Error("SYS MEM LIMIT REACHED [%d >= %d] - starting graceful restart", sysMemBytes, sysMemBytesLimit)
			a.StartGracefulRestart("Sys Memory limit reached")
		}
		if allocMemBytesLimit > 0 && allocMemBytes >= allocMemBytesLimit {
			a.Error("ALLOC MEM LIMIT REACHED [%d >= %d] - starting graceful restart", allocMemBytes, allocMemBytesLimit)
			a.StartGracefulRestart("Alloc Memory limit reached")
		}
		if numFDsLimit > 0 && numFDs >= numFDsLimit {
			a.Error("NUM FDS LIMIT REACHED [%d >= %d] - starting graceful restart", numFDs, numFDsLimit)
			a.StartGracefulRestart("Number of fds limit reached")
		}
		if numGorosLimit > 0 && numGoros >= numGorosLimit {
			a.Error("NUM GOROS LIMIT REACHED [%d >= %d] - starting graceful restart", numGoros, numGorosLimit)
			a.StartGracefulRestart("Number of goros limit reached")
		}

		restartAfterSecs, _ := a.Cfg.GetFloat32("gop", "restart_after_secs", 0)
		appRunTime := time.Since(a.startTime).Seconds()
		if restartAfterSecs > 0 && appRunTime > float64(restartAfterSecs) {
			a.Error("TIME LIMIT REACHED [%f >= %f] - starting graceful restart", appRunTime, restartAfterSecs)
			a.StartGracefulRestart("Run time limit reached")
		}
		<-ticker
	}
}

type responseWriter struct {
	http.ResponseWriter
	app       *App
	code      int
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

func (w *responseWriter) CloseNotify() <-chan bool {
	return w.ResponseWriter.(http.CloseNotifier).CloseNotify()
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

		err := r.ParseForm()
		if err != nil {
			a.Error("Failed to parse form: " + err.Error() + " (continuing)")
			//            http.Error(&gopWriter, "Failed to parse form: " + err.Error(), http.StatusInternalServerError)
			//          return
		}

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
	a.setProcessGroupForNelly()

	a.goAgainSetup()

	a.registerGopHandlers()

	go a.watchdog()

	go a.requestMaker()

	listenAddr, _ := a.Cfg.Get("gop", "listen_addr", ":http")
	listenNet, _ := a.Cfg.Get("gop", "listen_net", "tcp")
	a.goAgainListenAndServe(listenNet, listenAddr)
}

func (a *App) Serve(l net.Listener) {
	http.Serve(l, a.GorillaRouter)
}

func getMemInfo() (int64, int64) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	// There's lots of stuff in here.
	return int64(memStats.Sys), int64(memStats.Alloc)
}
