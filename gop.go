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
	"bufio"
	"encoding/json"
	"os"

	"github.com/gorilla/context"
	"github.com/gorilla/mux"
	"github.com/gorilla/schema"
	"github.com/gorilla/websocket"

	"fmt"
	"net"
	"net/http"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// Stuff we include in both App and Req, for convenience
type common struct {
	Logger
	Cfg     Config
	Stats   *StatsdClient
	Decoder *schema.Decoder
}

type AppStats struct {
	startTime     time.Time
	currentReqs   int
	currentWSReqs int
	totalReqs     int
}

func (as AppStats) String() string {
	uptime := time.Since(as.startTime)
	return fmt.Sprintf("Started at %s - uptime %s. %d reqs, %d ws %d total reqs",
		as.startTime,
		uptime,
		as.currentReqs,
		as.currentWSReqs,
		as.totalReqs)
}

// Represents a gop application. Create with gop.Init(projectName, applicationName)
type App struct {
	common

	AppName       string
	ProjectName   string
	GorillaRouter *mux.Router
	listener      net.Listener
	doingShutdown bool

	wantReq  chan *wantReq
	doneReq  chan *Req
	getReqs  chan chan *Req
	getStats chan chan AppStats

	accessLog                *os.File
	suppressedAccessLogLines int
	logDir                   string
	loggerMap                map[string]int
	logFormatterFactory      LogFormatterFactory
	configHandlersEnabled    bool
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
	// Only one of these is valid to use...
	W           *responseWriter
	WS          *websocket.Conn
	WsCloseChan chan struct{}
	CanBeSlow   bool //set this to true to suppress the "Slow Request" warning
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

type WebSocketCloseMessage struct {
	Code int
	Body string
}

func (h WebSocketCloseMessage) Error() string {
	return fmt.Sprintf("%d %s", h.Code, h.Body)
}

var CloseAbnormalClosure = WebSocketCloseMessage{Code: websocket.CloseAbnormalClosure}
var ClosePolicyViolation = WebSocketCloseMessage{Code: websocket.ClosePolicyViolation}
var CloseGoingAway = WebSocketCloseMessage{Code: websocket.CloseGoingAway}

func PolicyViolation(body string) error {
	err := ClosePolicyViolation
	err.Body = body
	return err
}

// Used for RPC to get a new request
type wantReq struct {
	r         *http.Request
	reply     chan *Req
	websocket bool
}

// Set up the application. Reads config. Panic if runtime environment is deficient.
func Init(projectName, appName, version string) *App {
	return doInit(projectName, appName, version, true, &TimberLogFormatterFactory{})
}

// For test code and command line tools
func InitCmd(projectName, appName, version string) *App {
	return doInit(projectName, appName, version, false, &TimberLogFormatterFactory{})
}

// Set up the application. Reads config. Panic if runtime environment is deficient.
func InitWithLogFormatter(
	projectName, appName, version string,
	logFormatterFactory LogFormatterFactory,
) *App {
	return doInit(projectName, appName, version, true, logFormatterFactory)
}

// For test code and command line tools
func InitCmdWithLogFormatter(
	projectName, appName, version string,
	logFormatterFactory LogFormatterFactory,
) *App {
	return doInit(projectName, appName, version, false, logFormatterFactory)
}

func doInit(
	projectName, appName, version string,
	requireConfig bool, logFormatterFactory LogFormatterFactory,
) *App {
	app := &App{
		common: common{
			Decoder: schema.NewDecoder(),
		},
		AppName:             appName,
		ProjectName:         projectName,
		GorillaRouter:       mux.NewRouter(),
		wantReq:             make(chan *wantReq),
		doneReq:             make(chan *Req),
		getReqs:             make(chan chan *Req),
		getStats:            make(chan chan AppStats),
		loggerMap:           make(map[string]int),
		logFormatterFactory: logFormatterFactory,
	}

	app.loadAppConfigFile(requireConfig)

	// Linux setuid() doesn't work with threaded procs :-O
	// and the go runtime threads before we can get going.
	//
	// http://homepage.ntlworld.com/jonathan.deboynepollard/FGA/linux-thread-problems.html
	//    app.setUserAndGroup()

	if version != "" {
		app.initLogging(version)
	} else {
		app.initLogging()
	}

	maxProcs, _ := app.Cfg.GetInt("gop", "maxprocs", 4*runtime.NumCPU())
	app.Debug("Setting maxprocs to %d", maxProcs)
	runtime.GOMAXPROCS(maxProcs)

	app.initStatsd()

	return app
}

// Hostname returns the apps hostname, os.Hostname() by default, but this can
// be overridden via gop.hostname config. This call is used when setting up
// logging and stats allowing a gop app to lie about it's hostname, useful in
// environments where the hostname may be the same across machines.
func (a *App) Hostname() string {
	if name, ok := a.Cfg.Get("gop", "hostname", ""); ok {
		return name
	}
	name, err := os.Hostname()
	if err != nil {
		// TODO - Is it safe to log here?
		name = "UNKNOWN"
	}
	return name
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

// Hands out 'request' objects
func (a *App) requestMaker() {
	nextReqId := 0
	openReqs := make(map[int]*Req)
	appStats := AppStats{startTime: time.Now()}

	for {
		select {
		case wantReq := <-a.wantReq:
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
			appStats.totalReqs++
			appStats.currentReqs++
			if wantReq.websocket {
				appStats.currentWSReqs++
			}
			a.Stats.Gauge("http_reqs", int64(appStats.totalReqs))
			a.Stats.Gauge("current_http_reqs", int64(appStats.currentReqs))
			a.Stats.Gauge("current_ws_reqs", int64(appStats.currentWSReqs))
			wantReq.reply <- &req
		case doneReq := <-a.doneReq:
			_, found := openReqs[doneReq.id]
			if !found {
				a.Errorf("BUG! Unknown request id [%d] being retired", doneReq.id)
			} else {
				doneReq.finished(appStats)
				appStats.currentReqs--
				a.Stats.Gauge("current_http_reqs", int64(appStats.currentReqs))
				if doneReq.WS != nil {
					appStats.currentWSReqs--
					a.Stats.Gauge("current_ws_reqs", int64(appStats.currentWSReqs))
				}
				delete(openReqs, doneReq.id)
			}
		case reply := <-a.getReqs:
			go func() {
				for _, req := range openReqs {
					reply <- req
				}
				close(reply)
			}()
		case statsReplyChan := <-a.getStats:
			go func() {
				statsReplyChan <- appStats
				close(statsReplyChan)
			}()
		}
	}
}

func (a *App) GetStats() AppStats {
	reqStats := make(chan AppStats)
	a.getStats <- reqStats
	appStats := <-reqStats // Caller closes after writing one value
	return appStats
}

// Uptime returns time instant the app was initialized.
func (a *App) StartTime() time.Time {
	return a.GetStats().startTime
}

// Ask requestMaker for a request
func (a *App) getReq(r *http.Request, websocket bool) *Req {
	reply := make(chan *Req)
	a.wantReq <- &wantReq{r: r, websocket: websocket, reply: reply}
	return <-reply
}

func (g *Req) finished(appStats AppStats) {
	context.Clear(g.R) // Cleanup  gorilla stash

	reqDuration := time.Since(g.startTime)

	g.app.WriteAccessLog(g, reqDuration)

	// Don't run time-based code for websockets
	if g.WS != nil {
		return
	}

	var code int
	if g.W != nil {
		code = g.W.code
	}
	codeStatsKey := fmt.Sprintf("http_status.%d", code)
	g.app.Stats.Inc(codeStatsKey, 1)

	slowReqSecs, _ := g.Cfg.GetFloat32("gop", "slow_req_secs", 10)
	if reqDuration.Seconds() > float64(slowReqSecs) && !g.CanBeSlow {
		g.Errorf("Slow request [%s] took %s", g.R.URL.Host, reqDuration)
	} else {
		g.Debug("Request took %s", reqDuration)
	}

	gcEveryReqs, _ := g.Cfg.GetInt("gop", "gc_requests", 0)
	if gcEveryReqs > 0 && appStats.totalReqs%gcEveryReqs == 0 {
		g.Info("Forcing GC after %d reqs", appStats.totalReqs)
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

// SendJson marshals the given v into JSON and sends it with the
// mimetype "application/json". what is a human-readable name for the
// thing being marshalled.
func (g *Req) SendJson(what string, v interface{}) error {
	json, err := json.Marshal(v)
	if err != nil {
		g.Errorf("Failed to encode %s as json: %s", what, err.Error())
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
	for k := range g.R.Form {
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
func (g *Req) ParamTime(key string) (time.Time, error) {
	s, err := g.Param(key)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, s)
}
func (g *Req) ParamBool(key string) (bool, error) {
	s, err := g.Param(key)
	if err != nil {
		return false, err
	}
	s = strings.ToLower(s)
	return (s == "1" || s == "true" || s == "yes"), nil
}

func (a *App) watchdog() {
	repeat, _ := a.Cfg.GetInt("gop", "watchdog_secs", 30)
	ticker := time.Tick(time.Second * time.Duration(repeat))

	firstLoop := true
	for {
		sysMemBytesLimit, _ := a.Cfg.GetInt64("gop", "sysmem_bytes_limit", 0)
		allocMemBytesLimit, _ := a.Cfg.GetInt64("gop", "allocmem_bytes_limit", 0)
		numFDsLimit, _ := a.Cfg.GetInt64("gop", "numfds_limit", 0)
		numGorosLimit, _ := a.Cfg.GetInt64("gop", "numgoros_limit", 0)

		sysMemBytes, allocMemBytes := getMemInfo()
		numFDs, err := fdsInUse()
		numGoros := int64(runtime.NumGoroutine())
		if err != nil {
			a.Debug("Failed to get number of fds in use: %s", err.Error())
			// Continue without
		}

		appStats := a.GetStats()
		gcStats := debug.GCStats{PauseQuantiles: make([]time.Duration, 3)}
		debug.ReadGCStats(&gcStats)
		gcMin := gcStats.PauseQuantiles[0]
		gcMedian := gcStats.PauseQuantiles[1]
		gcMax := gcStats.PauseQuantiles[2]
		a.Info("TICK: sys=%d,alloc=%d,fds=%d,current_req=%d,total_req=%d,goros=%d,gc=%v/%v/%v",
			sysMemBytes,
			allocMemBytes,
			numFDs,
			appStats.currentReqs,
			appStats.totalReqs,
			numGoros,
			gcMin,
			gcMedian,
			gcMax)
		if firstLoop {
			// Zero some gauges at start, otherwise restarts get lost in the graphs
			// and it looks like app is continously using memory.
			a.Stats.Gauge("mem.sys", 0)
			a.Stats.Gauge("mem.alloc", 0)
			a.Stats.Gauge("numfds", 0)
			a.Stats.Gauge("numgoro", 0)
		} else {
			a.Stats.Gauge("mem.sys", sysMemBytes)
			a.Stats.Gauge("mem.alloc", allocMemBytes)
			a.Stats.Gauge("numfds", numFDs)
			a.Stats.Gauge("numgoro", numGoros)
		}

		if sysMemBytesLimit > 0 && sysMemBytes >= sysMemBytesLimit {
			a.Errorf("SYS MEM LIMIT REACHED [%d >= %d] - exiting", sysMemBytes, sysMemBytesLimit)
			a.Shutdown("Sys Memory limit reached")
		}
		if allocMemBytesLimit > 0 && allocMemBytes >= allocMemBytesLimit {
			a.Errorf("ALLOC MEM LIMIT REACHED [%d >= %d] - exiting", allocMemBytes, allocMemBytesLimit)
			a.Shutdown("Alloc Memory limit reached")
		}
		if numFDsLimit > 0 && numFDs >= numFDsLimit {
			a.Errorf("NUM FDS LIMIT REACHED [%d >= %d] - exiting", numFDs, numFDsLimit)
			a.Shutdown("Number of fds limit reached")
		}
		if numGorosLimit > 0 && numGoros >= numGorosLimit {
			a.Errorf("NUM GOROS LIMIT REACHED [%d >= %d] - exiting", numGoros, numGorosLimit)
			a.Shutdown("Number of goros limit reached")
		}

		exitAfterSecs, _ := a.Cfg.GetFloat32("gop", "exit_after_secs", 0)
		appRunTime := time.Since(appStats.startTime).Seconds()
		if exitAfterSecs > 0 && appRunTime > float64(exitAfterSecs) {
			a.Errorf("TIME LIMIT REACHED [%f >= %f] - exiting", appRunTime, exitAfterSecs)
			a.Shutdown("Run time limit reached")
		}
		firstLoop = false
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

func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.ResponseWriter.(http.Hijacker).Hijack()
}

func dealWithPanic(g *Req, showInResponse, showInLog, showAllInBacktrace bool, panicHTTPMessage string) {
	r := recover()
	if r == nil {
		// We're only here to handle a panic
		return
	}

	// Build an error to write
	var errBody string
	var errCode int
	if g.WS != nil {
		errCode = websocket.CloseAbnormalClosure
	} else {
		errCode = http.StatusInternalServerError
	}

	sawHTTPErrorPanic := false

	// If we can get a string out of the recovered data, do so
	var recoveredMessage string
	switch r := r.(type) {
	case WebSocketCloseMessage:
		errCode = r.Code
		errBody = r.Body
		sawHTTPErrorPanic = true
	case HTTPError:
		errCode = r.Code
		errBody = r.Body
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
		errBody = panicHTTPMessage
		if errBody == "" {
			errBody = "PANIC: " + recoveredMessage
		}
	}

	if sawHTTPErrorPanic {
		g.Error("PANIC - sending panic'd error to client")
	} else if showInResponse {
		g.Error("PANIC - sending backtrace to client")
		errBody += "\n\n" + string(getBackTrace(showAllInBacktrace))
	} else {
		g.Error("PANIC - sending info to client")
	}

	if showInLog {
		g.Error("PANIC: " + recoveredMessage + string(getBackTrace(showAllInBacktrace)))
	}

	if g.WS != nil {
		g.webSocketClose(errCode, errBody)
	} else if g.W != nil && g.W.HasWritten() {
		g.Errorf("PANIC after handler had written data: %s", errBody)
	} else {
		httpErr := HTTPError{
			Code: errCode,
			Body: errBody,
		}
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
	return a.wrapHandlerInternal(h, false, requiredParams...)
}

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (a *App) wrapHandlerInternal(h HandlerFunc, isWebsocket bool, requiredParams ...string) http.HandlerFunc {
	panicHTTPMessage, _ := a.Cfg.Get("gop", "panic_http_message", "")
	showInLog, _ := a.Cfg.GetBool("gop", "panic_backtrace_to_log", false)
	showInResponse, _ := a.Cfg.GetBool("gop", "panic_backtrace_in_response", false)
	showAllInBacktrace, _ := a.Cfg.GetBool("gop", "panic_backtrace_all_goros", true)

	// Wrap the handler, so we can do before/after logic
	f := func(w http.ResponseWriter, r *http.Request) {
		gopRequest := a.getReq(r, isWebsocket)
		defer func() {
			a.doneReq <- gopRequest
		}()

		if isWebsocket {
			ws, err := wsUpgrader.Upgrade(w, r, nil)
			gopRequest.WS = ws
			if err != nil {
				errStr := "Failed to upgrade websocket " + err.Error()
				a.Error(errStr)
				http.Error(w, errStr, http.StatusInternalServerError)
				return
			}

			gopRequest.WsCloseChan = make(chan struct{})
			go func() {
				for {
					// TODO: expose the reader to the handler
					if _, _, err := gopRequest.WS.NextReader(); err != nil {
						gopRequest.WS.Close()
						close(gopRequest.WsCloseChan)
						break
					}
				}
			}()
		} else {
			gopWriter := responseWriter{code: 200, ResponseWriter: w}
			gopRequest.W = &gopWriter
		}

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
			if gopRequest.WS != nil {
				gopRequest.webSocketError(err)
			} else {
				httpErr, ok := err.(HTTPError)
				if !ok {
					httpErr = HTTPError{
						Code: http.StatusInternalServerError,
						Body: "Internal error: " + err.Error(),
					}
				}
				if gopRequest.W != nil && gopRequest.W.HasWritten() {
					// Ah. We have an error we'd like to send. But it's too late.
					// Bad handler, no biscuit.
					a.Errorf("Handler returned http error after writing data [%s] - discarding error", httpErr)
				} else {
					httpErr.Write(gopRequest.W)
				}
			}
		}
	}

	return http.HandlerFunc(f)
}

func (a *App) HTTPHandler(u string, h http.Handler) {
	f := func(g *Req) error {
		h.ServeHTTP(g.W, g.R)
		return nil
	}
	a.HandleFunc(u, f)
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

func (a *App) HandleMap(hm map[string]func(g *Req) error) {
	for k, v := range hm {
		a.HandleFunc(k, v)
	}
}

func (a *App) Run() {
	a.Start()
	a.ConfigServe()
}

func (a *App) Start() {
	a.registerGopHandlers()
	go a.watchdog()
	go a.requestMaker()
}

func (a *App) ConfigServe() {
	listenAddr, _ := a.Cfg.Get("gop", "listen_addr", ":http")
	listenNet, _ := a.Cfg.Get("gop", "listen_net", "tcp")

	listener, err := net.Listen(listenNet, listenAddr)
	if err != nil {
		a.Fatalf("Can't listen on [%s:%s]: %s", listenNet, listenAddr, err.Error())
	}
	a.Serve(listener)
}

func (a *App) Serve(l net.Listener) {
	a.listener = l
	http.Serve(a.listener, a.GorillaRouter)
}

func (a *App) Shutdown(reason string) {
	a.Infof("Shutting down: %s", reason)
	// Stop listening for new requests
	if a.listener != nil {
		a.listener.Close()
		a.Infof("Listener stopped")
	}
	// TODO: add back grace period for requests to exit
	a.Infof("Time to die")
	a.Finish()
	// TODO: allow control of exit code by caller
	os.Exit(0)
}

func getMemInfo() (int64, int64) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	// There's lots of stuff in here.
	return int64(memStats.Sys), int64(memStats.Alloc)
}

func (a *App) HandleWebSocketFunc(u string, h HandlerFunc, requiredParams ...string) *mux.Route {
	gopHandler := a.WrapWebSocketHandler(h, requiredParams...)

	return a.GorillaRouter.HandleFunc(u, gopHandler)
}

func (a *App) WrapWebSocketHandler(h HandlerFunc, requiredParams ...string) http.HandlerFunc {
	return a.wrapHandlerInternal(h, true, requiredParams...)
}

func (g *Req) WebSocketWriteText(buf []byte) error {
	return g.WS.WriteMessage(websocket.TextMessage, buf)
}

func (g *Req) WebSocketWriteBinary(buf []byte) error {
	return g.WS.WriteMessage(websocket.BinaryMessage, buf)
}

// webSocketClose initiates closing the websocket connection on
// the gop request and waits a short time for the WsCloseChan to close
// before forcefully closing the connection. This doesn't need to be called
// if the client closed the connection.
func (g *Req) webSocketClose(code int, text string) {
	data := websocket.FormatCloseMessage(code, text)
	if err := g.WS.WriteMessage(websocket.CloseMessage, data); err != nil {
		g.Errorf("write websocket close message: %v", err)
		g.WS.Close()
		return
	}

	select {
	case <-g.WsCloseChan:
	case <-time.After(time.Second):
		g.Debug("close websocket timeout")
		g.WS.Close()
	}
}

func (g *Req) webSocketError(err error) {
	var closeMessage WebSocketCloseMessage
	switch t := err.(type) {
	case WebSocketCloseMessage:
		closeMessage = t
	case HTTPError:
		closeMessage = websocketCloseMessageFromHTTPError(t)
	default:
		closeMessage = CloseAbnormalClosure
	}

	g.webSocketClose(closeMessage.Code, closeMessage.Body)
}

func websocketCloseMessageFromHTTPError(err HTTPError) WebSocketCloseMessage {
	var closeCode int
	switch err.Code / 100 {
	case 2:
		closeCode = websocket.CloseNormalClosure
	case 4:
		closeCode = websocket.ClosePolicyViolation
	case 5:
		closeCode = websocket.CloseInternalServerErr
	default:
		closeCode = websocket.CloseAbnormalClosure
	}

	return WebSocketCloseMessage{Code: closeCode, Body: err.Body}
}
