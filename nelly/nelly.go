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

// Embed so we can define our own methods here
type nelly struct {
    *gop.App
    exeName string
    pgid int
    sigChan chan os.Signal
}

func main() {
    n := loadNelly()

    proc := n.startChild()

	// The child has to call setpgid() to install itself as a process group leader
    // (i.e. it will have pgid == pid).
    // We can then monitor whether the process group has become empty or not.
    n.pgid = proc.Pid

    // We can send a signal to all members of a process group with kill() with a -ve pid
    // We can send a 'do nothing' signal with a sig of 0

    // So we can send a sig of 0 to a process group and see if the process group is empty
    // empty process group => we need to restart

    n.setupSignals(proc)

    checkSecs, _ := n.Cfg.GetFloat32("gop", "nelly_check_secs", 1.0)
    ticker := time.Tick(time.Second * time.Duration(checkSecs))
LOOP:
    for {
        select {
            case <- ticker: {
                if n.processGroupIsEmpty() {
                    n.Error("Process group [%d] empty", n.pgid)
                    break LOOP
                }
            }
            case sig := <- n.sigChan: {
                n.Error("Caught signal: %s - killing process group", sig)
                syscall.Kill(-n.pgid, syscall.SIGTERM)
                n.Error("Exiting on SIGTERM")
                os.Exit(0)
            }
        }
    }
    n.Error("Descendants are dead - exiting")
}

func (n *nelly) setupSignals(proc *os.Process) {
    n.sigChan = make(chan os.Signal, 10)     // 10 is arbitrary, we just need to keep up
//    signal.Notify(n.sigChan, syscall.SIGTERM, syscall.SIGKILL)
    signal.Notify(n.sigChan)
}

func (n *nelly) processGroupIsEmpty() bool {
    err := syscall.Kill(-n.pgid, syscall.Signal(0x00))
    if err != nil {
        n.Error("Kill error: %s\n", err.Error())
    }
    return err != nil
}

func loadNelly() *nelly {
    var exeName string
    flag.StringVar(&exeName, "exe", "", "Name of executable to run")

    var appName string
    flag.StringVar(&appName, "service", "", "Name of service to start")

    var projectName string
    flag.StringVar(&projectName, "project", "", "Name of project")

    flag.Parse()

	if projectName == "" {
		println("You must specify the name of a project with --project=project_name")
		os.Exit(1)
	}
	if appName == "" {
		println("You must specify the name of a gop service to run with --service=service_name")
		os.Exit(1)
	}
	if exeName == "" {
		println("You must specify the name of a gop exe to run with --exe=exe_name")
		os.Exit(1)
	}

    // We won't run gop, but load it up for config and logging
    a := gop.Init(projectName, appName) 

    // Wrap so we can have methods on our own type
    n := nelly{App: a, exeName: exeName}

    n.Info("nelly initialised for [%s:%s:%s]", projectName, appName, exeName)

    return &n
}

func (n *nelly) startChild() *os.Process {
    attr := new(os.ProcAttr)
    proc, err := os.StartProcess(n.exeName, nil, attr)
    if err != nil {
        panic(fmt.Sprintf("Failed to start process [%s]: %s", n.exeName, err.Error()))
    }
    n.Info("Started executable [%s] pid %d", n.exeName, proc.Pid)

    return proc
}

