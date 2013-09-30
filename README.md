# GOP

## Overview

GOP (GOlang-in-Production) is a collection of code intended to ease writing production-safe golang
applications, particularly web applications.

An app initialises GOP with a project name and appname and will then get access to various facilities
in a standardised way.

## Features

Provided facilities are:

### Logging

Log4j-like logging (no dynamic reconfig or per package) to /var/log/<project>/<app>log

### Configuration

An ini-file loaded from /etc/<project>/<app>.conf

A [gop] section in this file controls various settings for the behaviour of GOP itself.

### Graceful restart

Sending a SIGQUIT to the process will initiate a graceful restart. This will cause a new child to
fork and take over the listening socket. The parent will continue to run until any pending requests
have been processed, or a timeout is reached, and then exit.

### Resource tracking and limits

A watchdog writes basic resource and activity stats to the log file at a (configurable) period.

### Introspection and test requests

/gop/request-status will return information about the currently running requests

/gop/test?secs=X&kbytes=Y will allocate and touch Y bytes and then sleep for X seconds.

## Future work

### Foreground mode

Reconcile graceful restart with a process manager

### Harakiri and resource management

Restart after handling X requests or Y seconds

Track number of fds in use (write out in the TICK messages) and restart when limit hit

Force GC every N requests

### Logging improvements

Log rotation

Log config reload

Detail log setting by some mixture of package/func/file/line

### Permissions

Set UID and GUID from config at startup (default to project name)

[partially done. user=<username> honoured in gop config]

### Remote endpoint

Extract X-Forwarded-For and X-Forwarded-Proto info from nginx. Expose to app. Expose in request-status.

### IPv6

Test ip6 access behind nginx

### Stats

Decorate with specific GOP statsd measure
[current and total http reqs in place, TODO: status codes]

Add more detailed introspection:

- full GC stats)

- number of goros

- full goro stack dump?

### Signal handling

- log re-open

- config reload

- clean shutdown

### Abnormal request tracking

- log slow requests

- log failing requests

## TODO

Additional TODO bugs:

* set http timeouts (avoid fd leak over time)

* Track mean cpu-secs/request figure (and output in TICK and request-status)

* ~~(DONE) Track request duration (and output in request-status)~~

* Don't allow graceful restart within N secs of process start (and/or if there is already a graceful runnning in another proc)

* change GOP handler to take two args - gop.Request and gop.Response (as per http), which embed the http versions
and override as needed (e.g. setting status code). Once we've overridden to see status codes, add in commented-out statsd
reporting on status codes.

* ~~(DONE) add stdout-logging override for development~~

* add 'sent statsd op X' debug logging for development
