package gop

import (
	"net"
	"net/http"
)

func (a *App) goAgainSetup() {
}

func (a *App) goAgainListenAndServe(listenNet, listenAddr string) {
	a.Info("Windows NO GOAGAIN SUPPORT - starting listener on %s:%s", listenNet, listenAddr)
	// No parent, start our own listener
	l, err := net.Listen(listenNet, listenAddr)
	if err != nil {
		a.Fatalln(err)
	}
	a.Serve(l)
}
