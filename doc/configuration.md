# Configuration

These are optional settings in the [gop] section of your config file.

## Logging

* log_dir [string, default "/var/log"] - base dir for logging. Actual logging dir is <log_dir>/<project>.

* log_filename [bool, default false] - enable source file information in log lines

* log_file [string, default <logDir>/<app>.log] - full pathname to log file [since this is a full path, it overrids log_dir]

* log_level [string, default "INFO"] - logging level. Possible values: "NONE", "FINEST", "FINE", "DEBUG", "TRACE", "INFO", "WARNING", "ERROR", "CRITICAL".

* log_pattern [string, default "[%D %T] [%L] %M"] - the format string as used by the timber logging module

* access_log_enable [bool, default false] - turn on access logging (not needed if all access via a logging proxy)

* access_log_filename [string, default '<logDir>/<appName>-access.log'] - name of the access log, if enabled

* access_log_every [integer, default 0] - if nonzero, only log every N access log lines (set to 10 for 1-in-10)

* stdout_only_logging [bool, default false] - force all logging output to go to STDOUT only.

## Nelly

* nelly_check_secs [float, default 1.0] - time between checks for child process death

* nelly_startup_grace_checks [integer, default 5] - number of times a child can fail a check during startup. Useful if app has a slow startup phase

## Watchdog and limits

* watchdog_secs [integer, default 300] - number of seconds between check watchdog check on the values below.

* numfds_limit [integer, default 0] - if non-zero, fd limit at which a graceful restart is triggered.

* allocmem_bytes_limit [integer, default 0] - if non-zero, graceful restart if golang 'alloc' memstat goes over this.

* sysmem_bytes_limit [integer, default 0] - if non-zero, graceful restart if golang 'sys' memstat goes over this.

* restart_after_secs [integer, default 0] - if non-zero, graceful restart after this many secs of uptime

* max_requests [integer, default 0] - if non-zero, graceful restart after this many http requests

* numgoros_limit [integer, default 0] - if non-zero, graceful restart if at this count of goros (simulaneous, not lifetime)

* gc_requests [integer, default 0] - if non-zero, force a golang garbage collection every N http requests.

## Panic handling during HTTP requests

* panic_http_message [string, default ""] - Fixed message returned if a panic occurs in the HTTP handler. Default is to return a PANIC: %s msg with some relevant information.

* panic_backtrace_in_response [bool, default false] - include a backtrace in the HTTP response (overridden by panic_http_message).

* panic_backtrace_to_log [bool, default false] - write the panic backtrace to the log at ERROR level.

* panic_backtrace_all_goros [bool, default true] - include all goros in the backtrace (to log and HTTP). Set to false to just see the panic'ing goro's stack.

## HTTP and network

* listen_addr [string, default ":http"] - address on which to listen. Defaults to all interfaces, http port

* listen_net [string, default "tcp"] - unsure. See godoc net Listen() documentation.

* use_xf_headers [bool, default false] - trust the X-Forwarded-For and X-Forwarded-Proto HTTP headers

* slow_req_secs [float, 10] - number of seconds before a request is considered 'slow' (and so ERROR logged)

## Statsd

* statsd_hostport [string, default "localhost:8125"] - host:port for statsd

* statsd_rate [float, default 1.0] - proportion of statsd requests to actually send. Values from 0.0 -> 1.0.

## Misc

* maxprocs [integer, default 4*runtime.NumCPU()] - golang maxprocs setting. Number of OS threads to start with.

* enable_gop_urls [bool, default false] - enable the /gop url handlers [BUG! /gop/config always enabled. TODO: add netmark for permitted clients.]

* graceful_poll_msecs [integer, default 500] - how many millisecs to wait before checkings l re uetl ere

* graceful_wait_secs [integer, default 60] - max time to wait for graceful exit. will hard exit after this time.

