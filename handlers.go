package gop

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/pprof"
	"os"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/schema"
)

var decoder = schema.NewDecoder() // Single-instance so struct info cached

func gopHandler(g *Req) error {
	vars := mux.Vars(g.R)
	switch vars["action"] {
	case "status":
		return handleStatus(g)
	case "stack":
		return handleStack(g)
	case "mem":
		return handleMem(g)
	case "test":
		return handleTest(g)
	case "config":
		return handleConfig(g)
	default:
		return ErrNotFound
	}
}

func handleConfig(g *Req) error {
	// We can be called with and without section+key
	vars := mux.Vars(g.R)
	section := vars["section"]
	key := vars["key"]
	if g.R.Method == "PUT" {
		if section == "" {
			return BadRequest("No section in url")
		}
		if key == "" {
			return BadRequest("No key in url")
		}
		value, err := ioutil.ReadAll(g.R.Body)
		if err != nil {
			return ServerError("Failed to read value: " + err.Error())
		}

		if len(value) == 0 {
			//Empty body is usually the result of a missing type header and evaluates to an empty string,
			//which will prevent tellus from starting if the config setting is not string-valued
			return BadRequest("Empty request body - I'm assuming you didn't mean to do that.")
		}

		g.Cfg.PersistentOverride(section, key, string(value))
	}

	if section != "" {
		if key != "" {
			strVal, found := g.Cfg.Get(section, key, "")
			if found {
				return g.SendJson("config", strVal)
			}
			return NotFound("No such key in section")
		}

		sectionKeys := g.Cfg.SectionKeys(section)
		sectionMap := make(map[string]string)
		for _, key := range sectionKeys {
			strVal, _ := g.Cfg.Get(section, key, "")
			sectionMap[key] = strVal
		}
		return g.SendJson("config", sectionMap)
	}
	configMap := g.Cfg.AsMap()
	return g.SendJson("config", configMap)
}

func handleMem(g *Req) error {
	if g.R.Method == "POST" {
		type memParams struct {
			GCNow     int `schema:"gc_now"`
			GCPercent int `schema:"gc_percent"`
		}
		params := memParams{}
		err := g.Decoder.Decode(&params, g.R.Form)
		if err != nil {
			g.Error("Failed to decode params: " + err.Error())
			return ServerError("Failed to decode params: " + err.Error())
		}
		msg := "Adjusting mem system\n"
		if params.GCNow > 0 {
			info := "Running GC by request to handler"
			g.Info(info)
			msg += info + "\n"

			runtime.GC()
		}
		if params.GCPercent > 0 {
			oldVal := debug.SetGCPercent(params.GCPercent)
			info := fmt.Sprintf("Set GC%% to [%d] was [%d]", params.GCPercent, oldVal)
			g.Info(info)
			msg += info + "\n"
		}
		return g.SendText([]byte(msg))
	}
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	return g.SendJson("memstats", memStats)
}

func handleStack(g *Req) error {
	buf := make([]byte, 1024)
	traceLen := 0
	for {
		traceLen = runtime.Stack(buf, true)
		if traceLen < len(buf) {
			break
		}
		// Try a bigger buf
		buf = make([]byte, 2*len(buf))
	}
	appStats := g.app.GetStats()

	g.W.Write([]byte(fmt.Sprintf("Stack trace for %s:%s at %s\n", g.app.ProjectName, g.app.AppName, time.Now())))
	g.W.Write([]byte(appStats.String() + "\n\n"))
	g.W.Write(buf[:traceLen])
	return nil
}

func handleStatus(g *Req) error {
	type requestInfo struct {
		Id       int
		Method   string
		Url      string
		Duration float64
		RemoteIP string
		IsHTTPS  bool
	}
	type requestStatus struct {
		ProjectName   string
		AppName       string
		Pid           int
		StartTime     time.Time
		UptimeSeconds float64
		NumGoros      int
		RequestInfo   []requestInfo
	}
	appStats := g.app.GetStats()
	appDuration := time.Since(appStats.startTime).Seconds()
	status := requestStatus{
		ProjectName:   g.app.ProjectName,
		AppName:       g.app.AppName,
		Pid:           os.Getpid(),
		StartTime:     appStats.startTime,
		UptimeSeconds: appDuration,
		NumGoros:      runtime.NumGoroutine(),
	}
	reqChan := make(chan *Req)
	g.app.getReqs <- reqChan
	for req := range reqChan {
		reqDuration := time.Since(req.startTime)
		info := requestInfo{
			Id:       req.id,
			Method:   req.R.Method,
			Url:      req.R.URL.String(),
			Duration: reqDuration.Seconds(),
			RemoteIP: req.RealRemoteIP,
			IsHTTPS:  req.IsHTTPS,
		}
		status.RequestInfo = append(status.RequestInfo, info)
	}
	return g.SendJson("status", status)
	/*
	   fmt.Fprintf(w, "%s - %s PID %d up for %.3fs (%s)\n\n", g.app.ProjectName, g.app.AppName, os.Getpid(), appDuration, g.app.startTime)
	   for req := range reqChan {
	       reqDuration := time.Since(req.startTime).Seconds()
	       fmt.Fprintf(w, "%d: %.3f\t%s\t%s\n", req.id, reqDuration, req.r.Method, req.r.URL.String())
	   }
	*/
}

func handleTest(g *Req) error {
	type details struct {
		Kbytes int `schema:"kbytes"`
		Secs   int `schema:"secs"`
	}
	args := details{}
	err := g.Decoder.Decode(&args, g.R.Form)
	if err != nil {
		return ServerError("Failed to decode params: " + err.Error())
	}
	g.Debug("Test req - taking %d secs, %d KB", args.Secs, args.Kbytes)
	buf := make([]byte, args.Kbytes*1024)
	// Touch/do something with the mem to ensure it's actually allocated
	for i := range buf {
		buf[i] = 1
	}
	time.Sleep(time.Second * time.Duration(args.Secs))
	g.SendText([]byte(fmt.Sprintf("Slow request took additional %d secs and allocated additional %d KB\n", args.Secs, args.Kbytes)))
	return nil
}

func (a *App) registerGopHandlers() {
	a.HandleFunc("/gop/{action}", accessWrapper(gopHandler))
	a.HandleFunc("/gop/config/{section}", accessWrapper(handleConfig))
	a.HandleFunc("/gop/config/{section}/{key}", accessWrapper(handleConfig))

	a.maybeRegisterPProfHandlers()
	a.Cfg.AddOnChangeCallback(func(cfg *Config) { a.maybeRegisterPProfHandlers() })
}

func (a *App) maybeRegisterPProfHandlers() {
	if enableProfiling, _ := a.Cfg.GetBool("gop", "enable_profiling_urls", false); enableProfiling && !a.configHandlersEnabled {
		a.HandleFunc("/debug/pprof/cmdline", accessWrapper(func(g *Req) error {
			pprof.Cmdline(g.W, g.R)
			return nil
		}))

		a.HandleFunc("/debug/pprof/symbol", accessWrapper(func(g *Req) error {
			pprof.Symbol(g.W, g.R)
			return nil
		}))

		a.HandleFunc("/debug/pprof/profile", accessWrapper(func(g *Req) error {
			pprof.Profile(g.W, g.R)
			return nil
		}))

		a.HandleFunc("/debug/pprof/{profile_name}", accessWrapper(func(g *Req) error {
			vars := mux.Vars(g.R)
			h := pprof.Handler(vars["profile_name"])
			h.ServeHTTP(g.W, g.R)
			return nil
		}))
		a.configHandlersEnabled = true
	}
}

func accessWrapper(handler HandlerFunc) HandlerFunc {
	return func(g *Req) error {
		// We require that:
		// (a) enable_gop_urls be set in the config
		enabled, _ := g.Cfg.GetBool("gop", "enable_gop_urls", false)
		if !enabled {
			return NotFound("Not enabled")
		}
		// (b) optionally we can set a ?foo=bar requirement on the url
		tokenKey, _ := g.Cfg.Get("gop", "handler_access_key", "")
		tokenVal, _ := g.Cfg.Get("gop", "handler_access_value", "")
		if tokenKey != "" && tokenVal != "" {
			params := g.Params()
			val, ok := params[tokenKey]
			if !ok || val != tokenVal {
				return HTTPError{
					Code: http.StatusForbidden,
				}
			}
		}
		return handler(g)
	}
}
