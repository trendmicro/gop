package gop

import (
    "github.com/gorilla/mux"
    "github.com/gorilla/schema"
    "net/http"
    "fmt"
    "time"
)

var decoder = schema.NewDecoder()       // Single-instance so struct info cached

func gopHandler(g *Req, w http.ResponseWriter, r *http.Request) {
    enabled, _ := g.Cfg.GetBool("gop", "enable_gop_urls", false)
    if !enabled {
        http.Error(w, "Not enabled", http.StatusNotFound)
        return
    }
    vars := mux.Vars(r)
    switch vars["action"] {
        case "request-status": {
            handleRequestStatus(g, w, r)
            return
        }
        case "test": {
            handleTest(g, w, r)
            return
        }
        default: {
            http.Error(w, "Not Found", http.StatusNotFound)
            return
        }
    }
}

func handleRequestStatus(g *Req, w http.ResponseWriter, r *http.Request) {
    reqChan := make(chan *Req)
    g.app.getReqs <- reqChan
    for req := range reqChan {
        reqDuration := float64(time.Since(req.startTime).Nanoseconds()) / 1000000000
        fmt.Fprintf(w, "%d: %.3f\t%s\t%s\n", req.id, reqDuration, req.r.Method, req.r.URL.String())
    }
}

func handleTest(g *Req, w http.ResponseWriter, r *http.Request) {
    type details struct {
        Kbytes int `schema:"kbytes"`
        Secs int `schema:"secs"`
    }
    args := details{}
    r.ParseForm()
    err := decoder.Decode(&args, r.Form)
    if err != nil {
        http.Error(w, "Failed to decode params: " + err.Error(), http.StatusInternalServerError)
        return
    }
    g.Debug("Test req - taking %d secs, %d KB", args.Secs, args.Kbytes)
    buf := make([]byte, args.Kbytes * 1024)
    // Touch/do something with the mem to ensure it's actually allocated
    for i := range buf {
        buf[i] = 1
    }
    time.Sleep(time.Second * time.Duration(args.Secs))
    fmt.Fprintf(w, "Slow request took additional %d secs and allocated additional %d KB\n", args.Secs, args.Kbytes)
}

func (a *App) registerGopHandlers() {
    a.HandleFunc("/gop/{action}", gopHandler)
}
