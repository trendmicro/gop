package gop

import (
	"fmt"
	"github.com/gorilla/mux"
	"github.com/gorilla/schema"
	"io/ioutil"
	"net/http/pprof"
	"os"
	"runtime"
	"runtime/debug"
	"time"
)

var decoder = schema.NewDecoder() // Single-instance so struct info cached

func gopHandler(g *Req) error {
	enabled, _ := g.Cfg.GetBool("gop", "enable_gop_urls", false)
	if !enabled {
		return NotFound("Not enabled")
	}
	vars := mux.Vars(g.R)
	switch vars["action"] {
	case "status":
		{
			return handleStatus(g)
		}
	case "stack":
		{
			return handleStack(g)
		}
	case "mem":
		{
			return handleMem(g)
		}
	case "test":
		{
			return handleTest(g)
		}
	case "config":
		{
			return handleConfig(g)
		}
	default:
		{
			return ErrNotFound
		}
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
		g.Cfg.PersistentOverride(section, key, string(value))
	}

	if section != "" {
		if key != "" {
			strVal, found := g.Cfg.Get(section, key, "")
			if found {
				return g.SendJson("config", strVal)
			} else {
				return NotFound("No such key in section")
			}
		} else {
			sectionKeys := g.Cfg.SectionKeys(section)
			sectionMap := make(map[string]string)
			for _, key := range sectionKeys {
				strVal, _ := g.Cfg.Get(section, key, "")
				sectionMap[key] = strVal
			}
			return g.SendJson("config", sectionMap)
		}
	} else {
		configMap := g.Cfg.AsMap()
		return g.SendJson("config", configMap)
	}
	return nil
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
		return nil
	}
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	return g.SendJson("memstats", memStats)
}

func handleStack(g *Req) error {
	buf := make([]byte, 1024)
	for {
		traceLen := runtime.Stack(buf, true)
		if traceLen < len(buf) {
			break
		}
		// Try a bigger buf
		buf = make([]byte, 2*len(buf))
	}
	g.W.Write(buf)
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
	appDuration := time.Since(g.app.startTime).Seconds()
	status := requestStatus{
		ProjectName:   g.app.ProjectName,
		AppName:       g.app.AppName,
		Pid:           os.Getpid(),
		StartTime:     g.app.startTime,
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
	a.HandleFunc("/gop/{action}", gopHandler)
	a.HandleFunc("/gop/config/{section}", handleConfig)
	a.HandleFunc("/gop/config/{section}/{key}", handleConfig)

	a.maybeRegisterPProfHandlers()
	a.Cfg.AddOnChangeCallback(func(cfg *Config) { a.maybeRegisterPProfHandlers() })
}

func (a *App) maybeRegisterPProfHandlers() {
	if enableProfiling, _ := a.Cfg.GetBool("gop", "enable_profiling_urls", false); enableProfiling {
		a.HandleFunc("/pprof/cmdline", func(g *Req) error {
			pprof.Cmdline(g.W, g.R)
			return nil
		})

		a.HandleFunc("/pprof/symbol", func(g *Req) error {
			pprof.Symbol(g.W, g.R)
			return nil
		})

		a.HandleFunc("/pprof/profile", func(g *Req) error {
			pprof.Profile(g.W, g.R)
			return nil
		})

		a.HandleFunc("/pprof/{profile_name}", func(g *Req) error {
			vars := mux.Vars(g.R)
			h := pprof.Handler(vars["profile_name"])
			h.ServeHTTP(g.W, g.R)
			return nil
		})
	}
}
