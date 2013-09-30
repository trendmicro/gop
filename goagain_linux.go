package gop

import (
    "github.com/rcrowley/goagain"
    "github.com/jbert/timber"
    "net"
    "os"
    "time"
    "syscall"
)

func (a *App) StartGracefulRestart(reason string) {
    if a.doingGraceful {
        a.Debug("Ignoring graceful [%s] - already in graceful", reason)
        return
    }

    // Caller should ERROR the reason
    a.Info("Starting triggered graceful restart: %s", reason)
    myPid := os.Getpid()
    me, err := os.FindProcess(myPid)
    if err != nil {
        a.Error("Can't FindProcess myself - can't graceful restart. This probably won't end well: %s", err.Error())
        return
    }
    a.Info("Sending SIGUSR2 to %d", myPid)
    a.doingGraceful = true
    me.Signal(syscall.SIGUSR2)
}

func (a *App) goAgainSetup() {
    goagain.OnSIGUSR1 = func(l net.Listener) error {
        a.Info("SIGUSR1 received")
        return nil
    }
}

func (a *App) goAgainListenAndServe(listenNet, listenAddr string) {
	l, ppid, err := goagain.GetEnvs()

    if err != nil {
        a.Info("No parent - starting listener on %s:%s", listenNet, listenAddr)
        // No parent, start our own listener
        l, err = net.Listen(listenNet, listenAddr)
        a.Debug("Listener is %v err is %v", l, err)
		if err != nil {
			a.Fatalln(err)
		}
    } else {
        // We have a parent, and we're now listening. Tell them to shut down.
        a.Info("Child taking over from graceful parent. Killing ppid %d\n", ppid)
		if err := goagain.KillParent(ppid); nil != err {
			a.Fatalln(err)
		}
    }
    go func() {
        a.Serve(l)
    }()

	// Block the main goroutine awaiting signals.
	if err := goagain.AwaitSignals(l); nil != err {
		a.Fatalln(err)
    }

    a.Error("Signal received - starting exit or restart")

    // We're the parent. Our child has taken over the listening duties. We can close
    // off our listener and drain pending requests.
    l.Close()
    waitSecs, _ := a.Cfg.GetInt("gop", "graceful_wait_secs", 60)
    timeoutChan := time.After(time.Second * time.Duration(waitSecs))

    tickMillis, _ := a.Cfg.GetInt("gop", "graceful_poll_msecs", 500)
    tickChan := time.Tick(time.Millisecond * time.Duration(tickMillis))

    waiting := true
    for waiting {
        select {
            case <- timeoutChan: {
                a.Error("Graceful restart timed out after %d seconds - being less graceful and exiting", waitSecs)
                waiting = false
            }
            case <- tickChan: {
                if a.currentReqs == 0 {
                    a.Error("Graceful restart - no pending requests - time to die")
                    waiting = false
                } else {
                    a.Info("Graceful restart - tick still have %d pending reqs", a.currentReqs)
                }
            }
        }
    }

    a.Info("Graceful restart - with %d pending reqs", a.currentReqs)

    // Start a log flush
    timber.Close()
    // This sucks. Wait for logs to flush.
    time.Sleep(time.Second * 2)
}

