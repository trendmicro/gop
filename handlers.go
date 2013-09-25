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
        case "slow": {
            handleSlow(g, w, r)
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
        fmt.Fprintf(w, "%d:\t%s\t%s\n", req.id, req.r.Method, req.r.URL.String())
    }
}

func handleSlow(g *Req, w http.ResponseWriter, r *http.Request) {
    type slow struct {
        Secs int `schema:"secs"`
    }
    args := slow{}
    r.ParseForm()
    err := decoder.Decode(&args, r.Form)
    if err != nil {
        http.Error(w, "Failed to decode params: " + err.Error(), http.StatusInternalServerError)
        return
    }
    g.Debug("Deliberate slow req - duration %d secs", args.Secs)
    time.Sleep(time.Second * time.Duration(args.Secs))
    fmt.Fprintf(w, "Slow request took %d secs\n", args.Secs)
}

func (a *App) registerGopHandlers() {
    a.HandleFunc("/gop/{action}", gopHandler)
}
