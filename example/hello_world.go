package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cocoonlife/gop"
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
	app.HandleFunc("/", func(g *gop.Req) error {
		return g.SendText([]byte("Hello from " + myApp.name))
	})

	// HTTP Errors can be handled..
	app.HandleFunc("/notthere", func(g *gop.Req) error {
		return gop.NotFound(fmt.Sprintf("%s says there's nobody home", myApp.name))
	})

	// As can 'internal' errors
	app.HandleFunc("/deeperproblem", func(g *gop.Req) error {
		_, err := os.Stat("/tmp/mustnotexist")
		return err
	})

	// And deepseated personal issues
	app.HandleFunc("/nerfherder", func(g *gop.Req) error {
		panic("I have a bad feeling about this")
	})

	// You shouldn't panic after writing output...
	app.HandleFunc("/porkins", func(g *gop.Req) error {
		g.SendText([]byte("Writing away"))
		panic("You can't panic now!?")
	})

	// And don't do this...it's exception handling, and therefore bad
	app.HandleFunc("/obiwan", func(g *gop.Req) error {
		panic(gop.NotFound("These aren't the droids you're looking for"))
	})

	app.HandleFunc("/showparams", func(g *gop.Req) error {
		io.WriteString(g.W, "Params are: <ul>\n")
		for k, v := range g.Params() {
			io.WriteString(g.W, fmt.Sprintf("<li>%s = %s</li>\n", k, v))
		}
		io.WriteString(g.W, "</ul>\n")
		return nil
	})

	app.HandleFunc("/sleeper", func(g *gop.Req) error {
		sleepDuration, err := g.ParamDuration("secs")
		if err != nil {
			return gop.BadRequest("Need to supply a duration as 'secs'")
		}
		cn := g.W.CloseNotify()
		g.Error("About to sleep")
		select {
		case <-cn:
			g.Error("Caller closed connection")
		case <-time.After(sleepDuration):
			g.Error("Received timeout")
		}
		return g.SendText([]byte(fmt.Sprintf("Slept for %s\n", sleepDuration)))
	})

	app.HandleFunc("/reqparam", func(g *gop.Req) error {
		io.WriteString(g.W, "Params are: <ul>\n")
		for k, v := range g.Params() {
			io.WriteString(g.W, fmt.Sprintf("<li>%s = %s</li>\n", k, v))
		}
		io.WriteString(g.W, "</ul>\n")
		return nil
	}, "needed")

	app.Run()
}
