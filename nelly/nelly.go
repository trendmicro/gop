package main

import (
    "flag"
    "fmt"
    "os"
    "time"
    "syscall"
)

func main() {
    var exeName string
    flag.StringVar(&exeName, "service", "", "Name of service to start")
    var watchdogSecs float64
    flag.Float64Var(&watchdogSecs, "watchdog_secs", 1, "Number of seconds between checks")
    flag.Parse()

	if exeName == "" {
		println("You must specify the name of a gop exe to run with --service=exe_name")
		os.Exit(1)
	}

    attr := new(os.ProcAttr)
    proc, err := os.StartProcess(exeName, nil, attr)
    if err != nil {
        panic(fmt.Sprintf("Failed to start process [%s]: %s\n", exeName, err.Error()))
    }
    fmt.Printf("Started [%s] pid %d\n", exeName, proc.Pid)

	// The child has to call setpgrp() to install itself as a process group leader. We can
	// then monitor whether the process group has become empty or not.

    // We can send a signal to all members of a process group with kill() with a -ve pid
    // We can send a 'do nothing' signal with a sig of 0

    // So we can send a sig of 0 to a process group and see if the process group is empty
    // empty process group => we need to restart

    ticker := time.Tick(time.Second * time.Duration(watchdogSecs))
    for {
        // Wait at least one tick, so the child has time to change it's
        // process group to be the same as it's pid
        if processGroupEmpty(proc.Pid) {
            fmt.Printf("Process group empty\n")
            break
        }
        <- ticker
    }
    fmt.Printf("Descendants are dead - exiting\n")
}

func processGroupEmpty(pgid int) bool {
    err := syscall.Kill(-pgid, syscall.Signal(0x00))
    if err != nil {
        fmt.Printf("Kill error: %s\n", err.Error())
    }
    return err != nil
}
