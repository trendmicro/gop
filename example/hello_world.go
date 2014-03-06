package main

import (
	"github.com/trendmicro/gop"
	"fmt"
	"os"
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
	app.HandleFunc("/", func(req *gop.Req) error {
		return req.SendText([]byte("Hello from " + myApp.name))
	})

	// HTTP Errors can be handled..
	app.HandleFunc("/notthere", func(req *gop.Req) error {
		return gop.NotFound(fmt.Sprintf("%s says there's nobody home", myApp.name))
	})

	// As can 'internal' errors
	app.HandleFunc("/deeperproblem", func(req *gop.Req) error {
		_, err := os.Stat("/tmp/mustnotexist")
		return err
	})

	// And deepseated personal issues
	app.HandleFunc("/nerfherder", func(req *gop.Req) error {
		panic("I have a bad feeling about this")
	})

	// And deepseated personal issues
	app.HandleFunc("/porkins", func(req *gop.Req) error {
		req.SendText([]byte("Writing away"))
		panic("You can't panic now!?")
	})

	app.Run()
}
