package gop

import (
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"net/http"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func (a *App) HandleWebSocketFunc(u string, h HandlerFunc, requiredParams ...string) *mux.Route {
	gopHandler := a.WrapWebSocketHandler(h, requiredParams...)

	return a.GorillaRouter.HandleFunc(u, gopHandler)
}

func (a *App) WrapWebSocketHandler(h HandlerFunc, requiredParams ...string) http.HandlerFunc {
	return a.wrapHandlerInternal(h, true, requiredParams...)
}

func (g *Req) WebSocketWrite(buf []byte) error {
	return g.WS.WriteMessage(websocket.TextMessage, buf)
}
