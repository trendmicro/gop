# Gop

Gop (Go-in-Production) is an application container framework.

Gop makes it easier to write, deploy and maintain Go applications in a
production environment.

Gop strongly follows convention-over-configuration principles. Things
like file locations and formats are standardized wherever it makes
sense for them to be.

## Features

Gop applications get the following features:

* Config file
* Logging
* HTTP microframework
* Graceful restarts
* Process manager
* Resource tracking and limits
* Runtime config overrides
* Introspection
* Go runtime control

More information about each of these features is [below](#contents).

## Getting started

Gop requires **go 1.2** or higher.

First, install the gop package:

    go get github.com/trendmicro/gop

Gop applications are identified by a **project name** and an **app
name**. Let's imagine our app is called *helloworld* and is part of a
project called *gopexamples*.

Create `helloworld.go`:

~~~go
package main

import (
	"github.com/trendmicro/gop"
)

// hello is a basic HTTP handler
func hello(g *gop.Req) error {
	return g.SendText([]byte("Hello, world!\n"))
}

// main initializes the gop app
func main() {
	app := gop.Init("gopexamples", "helloworld")
	app.HandleFunc("/hello", hello)
	app.Run()
}
~~~

Every gop app has a config file.

Create `/etc/gopexamples/helloworld.conf`. The filename is
important. It must match the project and app names of your app.

~~~ini
[gop]
listen_addr=:8888
stdout_only_logging=true
~~~

And run your app:

    go run helloworld.go

You should be able to access your handler at <http://localhost:8888/hello>.

## Getting help

Issues and bugs can be reported at the
[Github issue tracker for gop](https://github.com/trendmicro/gop/issues).

## Contents

* [Configuration](doc/configuration.md)
* [HTTP handlers](doc/http_handlers.md)
* [Logging](doc/logging.md)
* [Process management](doc/process_management.md)
* [Resource management](doc/resource_management.md)
* [Introspection & management interface](doc/gop_urls.md)

## Authors

* John Berthels
* Mark Boyd
