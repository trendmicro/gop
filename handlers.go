package gop

import (
	"fmt"
	"github.com/gorilla/mux"
	"github.com/gorilla/schema"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"time"
)

var decoder = schema.NewDecoder() // Single-instance so struct info cached

func gopHandler(g *Req, w http.ResponseWriter, r *http.Request) {
	enabled, _ := g.Cfg.GetBool("gop", "enable_gop_urls", false)
	if !enabled {
		http.Error(w, "Not enabled", http.StatusNotFound)
		return
	}
	vars := mux.Vars(r)
	switch vars["action"] {
	case "status":
		{
			handleStatus(g, w, r)
			return
		}
	case "stack":
		{
			handleStack(g, w, r)
			return
		}
	case "mem":
		{
			handleMem(g, w, r)
			return
		}
	case "test":
		{
			handleTest(g, w, r)
			return
		}
	case "config":
		{
			handleConfig(g, w, r)
			return
		}
	default:
		{
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
	}
}

func handleConfig(g *Req, w http.ResponseWriter, r *http.Request) {
	// We can be called with and without section+key
	vars := mux.Vars(r)
	if r.Method == "PUT" {
		section := vars["section"]
		key := vars["key"]
		if section == "" {
			http.Error(w, "No section in url", http.StatusBadRequest)
			return
		}
		if key == "" {
			http.Error(w, "No key in url", http.StatusBadRequest)
			return
		}
		value, err := ioutil.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read value: "+err.Error(), http.StatusInternalServerError)
			return
		}
		g.Cfg.PersistentOverride(section, key, string(value))
	}

	configMap := g.Cfg.AsMap()
	g.SendJson(w, "config", configMap)
}

func handleMem(g *Req, w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		type memParams struct {
			GCNow     int `schema:"gc_now"`
			GCPercent int `schema:"gc_percent"`
		}
		params := memParams{}
		err := g.Decoder.Decode(&params, r.Form)
		if err != nil {
			g.Error("Failed to decode params: "+err.Error(), http.StatusInternalServerError)
			http.Error(w, "Failed to decode params: "+err.Error(), http.StatusInternalServerError)
			return
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
		io.WriteString(w, msg)
		return
	}
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	g.SendJson(w, "memstats", memStats)
}

func handleStack(g *Req, w http.ResponseWriter, r *http.Request) {
	buf := make([]byte, 1024)
	for {
		traceLen := runtime.Stack(buf, true)
		if traceLen < len(buf) {
			break
		}
		// Try a bigger buf
		buf = make([]byte, 2*len(buf))
	}
	w.Write(buf)
}

func handleStatus(g *Req, w http.ResponseWriter, r *http.Request) {
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
			Method:   req.r.Method,
			Url:      req.r.URL.String(),
			Duration: reqDuration.Seconds(),
			RemoteIP: req.RealRemoteIP,
			IsHTTPS:  req.IsHTTPS,
		}
		status.RequestInfo = append(status.RequestInfo, info)
	}
	g.SendJson(w, "status", status)
	/*
	   fmt.Fprintf(w, "%s - %s PID %d up for %.3fs (%s)\n\n", g.app.ProjectName, g.app.AppName, os.Getpid(), appDuration, g.app.startTime)
	   for req := range reqChan {
	       reqDuration := time.Since(req.startTime).Seconds()
	       fmt.Fprintf(w, "%d: %.3f\t%s\t%s\n", req.id, reqDuration, req.r.Method, req.r.URL.String())
	   }
	*/
}

func handleTest(g *Req, w http.ResponseWriter, r *http.Request) {
	type details struct {
		Kbytes int `schema:"kbytes"`
		Secs   int `schema:"secs"`
	}
	args := details{}
	err := g.Decoder.Decode(&args, r.Form)
	if err != nil {
		http.Error(w, "Failed to decode params: "+err.Error(), http.StatusInternalServerError)
		return
	}
	g.Debug("Test req - taking %d secs, %d KB", args.Secs, args.Kbytes)
	buf := make([]byte, args.Kbytes*1024)
	// Touch/do something with the mem to ensure it's actually allocated
	for i := range buf {
		buf[i] = 1
	}
	time.Sleep(time.Second * time.Duration(args.Secs))
	fmt.Fprintf(w, "Slow request took additional %d secs and allocated additional %d KB\n", args.Secs, args.Kbytes)
}

func (a *App) registerGopHandlers() {
	a.HandleFunc("/gop/{action}", gopHandler)
	a.HandleFunc("/gop/config/{section}/{key}", handleConfig)
}
