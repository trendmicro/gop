package main

import (
	"github.com/trendmicro/gop"
	"net/http"
	"fmt"
)

type MyApp struct {
	name string
}

func main() {
	// Get logging and config going
	app := gop.Init("hello", "world")
	
	// Construct our global context. Make this readonly or be prepared to
	// syncronise 
	myApp := MyApp{name: "Greeter"}

	// Register our handler, closing over the global context
	app.HandleFunc("/", func(req *gop.Req, w http.ResponseWriter, r *http.Request) error {
		return req.SendText(w, []byte("Hello from " + myApp.name))
	})

	// Errors can be handled..
	app.HandleFunc("/notthere", func(req *gop.Req, w http.ResponseWriter, r *http.Request) error {
		return gop.NotFound(fmt.Sprintf("%s says there's nobody home", myApp.name))
	})

	app.Run()
}
