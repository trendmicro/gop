/*
	GOP "Go in Production" is an attempt to provide a useful set of services for running
	(primarily http) applications in production service.

	This includes:
		- configuration
		- logging
		- statsd integration
		- signal handling
		- resource management
		- basic web framework
*/
package gop

import (
	"encoding/json"
	"github.com/gorilla/context"
	"github.com/gorilla/mux"
	"github.com/gorilla/schema"
	"os"

	"fmt"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Stuff we include in both App and Req, for convenience
type common struct {
	Logger
	loggerIndex int
	Cfg         Config
	Stats       StatsdClient
	Decoder     *schema.Decoder
}

// Represents a gop application. Create with gop.Init(projectName, applicationName)
type App struct {
	common

	AppName                  string
	ProjectName              string
	GorillaRouter            *mux.Router
	listener                 net.Listener
	wantReq                  chan *wantReq
	doneReq                  chan *Req
	getReqs                  chan chan *Req
	startTime                time.Time
	currentReqs              int
	totalReqs                int
	doingGraceful            bool
	accessLog                *os.File
	suppressedAccessLogLines int
	logDir                   string
}

// The function signature your http handlers need.
type HandlerFunc func(g *Req) error

// Per request struct. has convenience references to functionality
// in the app singleton. Passed into the request handler.
type Req struct {
	common

	id           int
	startTime    time.Time
	app          *App
	R            *http.Request
	RealRemoteIP string
	IsHTTPS      bool
	W            *responseWriter
	CanBeSlow    bool //set this to true to suppress the "Slow Request" warning
}

// Return one of these from a handler to control the error response
// Returning nil if you have sent your own response (as is typical on success)
type HTTPError struct {
	Code int
	Body string
}

// To satisfy the interface only
func (h HTTPError) Error() string {
	return fmt.Sprintf("HTTP Error [%d] - %s", h.Code, h.Body)
}
func (h HTTPError) Write(w *responseWriter) {
	w.WriteHeader(h.Code)
	w.Write([]byte(h.Body))
	w.Write([]byte("\r\n"))
}

// Simple helpers for common HTTP error cases
var ErrNotFound HTTPError = HTTPError{Code: http.StatusNotFound}
var ErrBadRequest HTTPError = HTTPError{Code: http.StatusBadRequest}
var ErrServerError HTTPError = HTTPError{Code: http.StatusInternalServerError}

// Helper to generate a NotFound HTTPError
func NotFound(body string) error {
	err := ErrNotFound
	err.Body = body
	return error(err)
}

// Helper to generate a BadRequest HTTPError
func BadRequest(body string) error {
	err := ErrBadRequest
	err.Body = body
	return error(err)
}

// Helper to generate an InternalServerError HTTPError
func ServerError(body string) error {
	err := ErrServerError
	err.Body = body
	return error(err)
}

// Used for RPC to get a new request
type wantReq struct {
	r     *http.Request
	reply chan *Req
}

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

// Shut down the app cleanly. (Needed to flush logs)
func (a *App) Finish() {
	// Start a log flush
	a.closeLogging()
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
					R:            wantReq.r,
					RealRemoteIP: realRemoteIP,
					IsHTTPS:      isHTTPS,
				}
				openReqs[req.id] = &req
				nextReqId++
				a.totalReqs++
				a.currentReqs++
				a.Stats.Gauge("http_reqs", int64(a.totalReqs))
				a.Stats.Gauge("current_http_reqs", int64(a.currentReqs))
				wantReq.reply <- &req
			}
		case doneReq := <-a.doneReq:
			{
				_, found := openReqs[doneReq.id]
				if !found {
					a.Error("BUG! Unknown request id [%d] being retired")
				} else {
					doneReq.finished()
					a.currentReqs--
					a.Stats.Gauge("current_http_reqs", int64(a.currentReqs))
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
	context.Clear(g.R) // Cleanup  gorilla stash

	reqDuration := time.Since(g.startTime)

	g.app.WriteAccessLog(g, reqDuration)

	codeStatsKey := fmt.Sprintf("http_status.%d", g.W.code)
	g.app.Stats.Inc(codeStatsKey, 1)

	slowReqSecs, _ := g.Cfg.GetFloat32("gop", "slow_req_secs", 10)
	if reqDuration.Seconds() > float64(slowReqSecs) && !g.CanBeSlow {
		g.Error("Slow request [%s] took %s", g.R.URL, reqDuration)
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

// send is the internal sends the given []byte with the specified MIME
// type to the specified ResponseWriter.
func (g *Req) send(mimetype string, v []byte) error {
	g.W.Header().Set("Content-Type", mimetype)
	g.W.Write(v)
	return nil
}

// SendText sends the given []byte with the mimetype "text/plain". The
// []byte must be in UTF-8 encoding.
func (g *Req) SendText(v []byte) error {
	return g.send("text/plain; charset=utf-8", v)
}

// SendHtml sends the given []byte with the mimetype "text/html". The
// []byte must be in UTF-8 encoding.
func (g *Req) SendHtml(v []byte) error {
	return g.send("text/html; charset=utf-8", v)
}

// SendHtml sends the given []byte with the mimetype "application/octet-stream".
func (g *Req) SendOctet(v []byte) error {
	return g.send("application/octet-stream", v)
}

// SendJson marshals the given v into JSON and sends it with the
// mimetype "application/json". what is a human-readable name for the
// thing being marshalled.
func (g *Req) SendJson(what string, v interface{}) error {
	json, err := json.Marshal(v)
	if err != nil {
		g.Error("Failed to encode %s as json: %s", what, err.Error())
		return ServerError("Failed to encode json: " + err.Error())
	}
	return g.send("application/json", append(json, '\n'))
}

func (g *Req) Params() map[string]string {
	err := g.R.ParseForm()
	if err != nil {
		g.Error("Failed to parse form: " + err.Error() + " (continuing)")
	}
	simpleParams := make(map[string]string)
	for k, _ := range g.R.Form {
		// Just pluck out the first
		simpleParams[k] = g.R.Form[k][0]
	}
	return simpleParams
}

func (g *Req) Param(key string) (string, error) {
	s, ok := g.Params()[key]
	if !ok {
		return "", ErrNotFound
	}
	return s, nil
}
func (g *Req) ParamInt(key string) (int, error) {
	s, err := g.Param(key)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}
func (g *Req) ParamDuration(key string) (time.Duration, error) {
	s, err := g.Param(key)
	if err != nil {
		return 0, err
	}
	return time.ParseDuration(s)
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

// Keep track of the status code and #bytes we write, so we can log and statsd on them
type responseWriter struct {
	http.ResponseWriter
	size int
	code int
}

// Satisfy the interface
func (w *responseWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

func (w *responseWriter) Write(buf []byte) (int, error) {
	w.size += len(buf)
	return w.ResponseWriter.Write(buf)
}

func (w *responseWriter) CloseNotify() <-chan bool {
	return w.ResponseWriter.(http.CloseNotifier).CloseNotify()
}

func (w *responseWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) HasWritten() bool {
	return w.size > 0
}

func dealWithPanic(g *Req, showInResponse, showInLog, showAllInBacktrace bool, panicHTTPMessage string) {
	r := recover()
	if r == nil {
		// We're only here to handle a panic
		return
	}

	// Build an error to write
	httpErr := HTTPError{
		Code: http.StatusInternalServerError,
	}
	sawHTTPErrorPanic := false

	// If we can get a string out of the recovered data, do so
	var recoveredMessage string
	switch r := r.(type) {
	case HTTPError:
		httpErr = r
		sawHTTPErrorPanic = true
	case error:
		recoveredMessage = r.Error()
	case fmt.Stringer:
		recoveredMessage = r.String()
	case string:
		recoveredMessage = r
	default:
		recoveredMessage = fmt.Sprintf("Unrecognised error: %v", r)
	}

	// Use custom panic message if we have one (and no panic'd HTTPError)
	if !sawHTTPErrorPanic {
		httpErr.Body = panicHTTPMessage
		if httpErr.Body == "" {
			httpErr.Body = "PANIC: " + recoveredMessage
		}
	}

	if sawHTTPErrorPanic {
		g.Error("PANIC - sending panic'd error to client")
	} else if showInResponse {
		g.Error("PANIC - sending backtrace to client")
		httpErr.Body += "\n\n" + string(getBackTrace(showAllInBacktrace))
	} else {
		g.Error("PANIC - sending info to client")
	}

	if showInLog {
		g.Error("PANIC: " + string(getBackTrace(showAllInBacktrace)))
	}

	if g.W.HasWritten() {
		g.Error("PANIC after handler had written data: %s", httpErr.Body)
	} else {
		httpErr.Write(g.W)
	}
}

func getBackTrace(showAllGoros bool) []byte {
	bufSize := 4096
	buf := make([]byte, bufSize)
	for {
		numWritten := runtime.Stack(buf, showAllGoros)
		if numWritten < bufSize {
			break
		}
		bufSize *= 2
	}
	return buf
}

func (a *App) WrapHandler(h HandlerFunc, requiredParams ...string) http.HandlerFunc {
	panicHTTPMessage, _ := a.Cfg.Get("gop", "panic_http_message", "")
	showInLog, _ := a.Cfg.GetBool("gop", "panic_backtrace_to_log", false)
	showInResponse, _ := a.Cfg.GetBool("gop", "panic_backtrace_in_response", false)
	showAllInBacktrace, _ := a.Cfg.GetBool("gop", "panic_backtrace_all_goros", true)

	// Wrap the handler, so we can do before/after logic
	f := func(w http.ResponseWriter, r *http.Request) {
		gopRequest := a.getReq(r)
		defer func() {
			a.doneReq <- gopRequest
		}()
		gopWriter := responseWriter{code: 200, ResponseWriter: w}
		gopRequest.W = &gopWriter

		// TODO: remove this. We call in Params() on demand. Need to move current code
		// over .Params() before we can remove this though.
		err := r.ParseForm()
		if err != nil {
			a.Error("Failed to parse form: " + err.Error() + " (continuing)")
			//            http.Error(&gopWriter, "Failed to parse form: " + err.Error(), http.StatusInternalServerError)
			//          return
		}

		// Panic handler
		defer dealWithPanic(gopRequest, showInResponse, showInLog, showAllInBacktrace, panicHTTPMessage)

		err = gopRequest.checkRequiredParams(requiredParams)
		// Only run handler if required args ok
		if err == nil {
			// Pass in the gop, for logging, cfg etc
			err = h(gopRequest)
		}

		if err != nil {
			httpErr, ok := err.(HTTPError)
			if !ok {
				httpErr = HTTPError{
					Code: http.StatusInternalServerError,
					Body: "Internal error: " + err.Error(),
				}
			}
			if gopWriter.HasWritten() {
				// Ah. We have an error we'd like to send. But it's too late.
				// Bad handler, no biscuit.
				a.Error("Handler returned http error after writing data [%s] - discarding error", httpErr)
			} else {
				httpErr.Write(&gopWriter)
			}
		}
	}

	return http.HandlerFunc(f)
}

func (g *Req) checkRequiredParams(requiredParams []string) error {
	if len(requiredParams) == 0 {
		return nil
	}
	params := g.Params()
	for _, requiredParam := range requiredParams {
		_, ok := params[requiredParam]
		if !ok {
			return HTTPError{
				Code: http.StatusBadRequest,
				Body: "Missing required parameter: " + requiredParam,
			}
		}
	}
	return nil
}

// Register an http handler managed by gop.
// We use Gorilla muxxer, since it is back-compatible and nice to use :-)
func (a *App) HandleFunc(u string, h HandlerFunc, requiredParams ...string) *mux.Route {
	gopHandler := a.WrapHandler(h, requiredParams...)

	return a.GorillaRouter.HandleFunc(u, gopHandler)
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
