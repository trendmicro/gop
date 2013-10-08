package main

import (
    "github.com/trendmicro/gop"

    "flag"
    "fmt"
    "os"
    "os/signal"
    "time"
    "syscall"
)

func main() {
    var appName string
    flag.StringVar(&appName, "service", "", "Name of service to start")

    var projectName string
    flag.StringVar(&projectName, "project", "", "Name of project")

	if projectName == "" {
		println("You must specify the name of a projec with --project=project_name")
		os.Exit(1)
	}
	if appName == "" {
		println("You must specify the name of a gop exe to run with --service=exe_name")
		os.Exit(1)
	}

    // We won't run gop, but load it up for config and logging
    a := gop.Init(projectName, appName) 

    checkSecs, _ := a.Cfg.GetFloat32("gop", "nelly_check_secs", 1.0)

    a.Info("nelly initialised for [%s:%s]", projectName, appName)

    attr := new(os.ProcAttr)
    proc, err := os.StartProcess(appName, nil, attr)
    if err != nil {
        panic(fmt.Sprintf("Failed to start process [%s]: %s", appName, err.Error()))
    }
    a.Info("Started executable [%s] pid %d", appName, proc.Pid)

	// The child has to call setpgrp() to install itself as a process group leader. We can
	// then monitor whether the process group has become empty or not.

    // We can send a signal to all members of a process group with kill() with a -ve pid
    // We can send a 'do nothing' signal with a sig of 0

    // So we can send a sig of 0 to a process group and see if the process group is empty
    // empty process group => we need to restart

    sigChan := setupSignals(a, proc)

    pgid := proc.Pid

    ticker := time.Tick(time.Second * time.Duration(checkSecs))
LOOP:
    for {
        select {
            case <- ticker: {
                if processGroupEmpty(a, pgid) {
                    a.Error("Process group [%d] empty", pgid)
                    break LOOP
                }
            }
            case sig := <- sigChan: {
                a.Error("Caught signal: %s - killing process group", sig)
                syscall.Kill(-pgid, syscall.SIGTERM)
                a.Error("Exiting on SIGTERM")
                os.Exit(0)
            }
        }
    }
    a.Error("Descendants are dead - exiting")
}

func setupSignals(a *gop.App, proc *os.Process) chan os.Signal {
    sigChan := make(chan os.Signal, 10)     // 10 is arbitrary, we just need to keep up
    signal.Notify(sigChan, syscall.SIGTERM)
    return sigChan
}

func processGroupEmpty(a *gop.App, pgid int) bool {
    err := syscall.Kill(-pgid, syscall.Signal(0x00))
    if err != nil {
        a.Error("Kill error: %s\n", err.Error())
    }
    return err != nil
}
