package main

import (
	"github.com/trendmicro/gop"

	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Embed so we can define our own methods here
type nelly struct {
	*gop.App
	ownThePidFile   bool
	exeName         string
	pgid            int
	sigChan         chan os.Signal
	startGracePings int
}

func main() {
	n := loadNelly()
	defer n.Finish()

	if !n.okToStart() {
		n.Error("Not ok to start - exiting")
		return
	}

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
	// Number of times we'll allow the child to miss a ping at startup,
	// which seems to be necessary since 1s isn't long enough for midserver
	// to get as far as setpgrp on smoke, apparently
	n.startGracePings, _ = n.Cfg.GetInt("gop", "nelly_startup_grace_checks", 5)

LOOP:
	for {
		select {
		case <-ticker:
			{
				if n.checkForDeath() {
					break LOOP
				}
			}
		case sig := <-n.sigChan:
			{
				if sig == syscall.SIGCHLD {
					// SIGCHLD is a signal from a child that it has
					// died - let's handle that immediately (if we
					// miss it we'll get it on the next tick)
					if n.checkForDeath() {
						break LOOP
					}
				} else {
					// Be hypersensitive and treat all other signals
					// as an indication that we need to shut down (in
					// the future we can tweak this list)

					n.Error("Caught signal: %s - killing process group", sig)
					syscall.Kill(-n.pgid, syscall.SIGTERM)
					n.Error("Exiting on signal %s", sig)
					os.Exit(0)
				}
			}
		}
	}
	n.Error("Descendants are dead - exiting")
}

// checkForDeath tries to clean up zombies and then checks if the
// process group is empty.
//
// It returns "true" if the answer is yes and there are no grace pings left
//
func (n *nelly) checkForDeath() bool {
	
	// Check if there are any zombies to eat. Process.Wait() doesn't
	// support the POSIX WNOHANG for portability reasons, so let's use
	// the syscall.Wait4() which is POSIX-only.
	var w syscall.WaitStatus
	rusage := syscall.Rusage{}
	zpid, err := syscall.Wait4(-1, &w, syscall.WNOHANG, &rusage)
	if err != nil {
		n.Error("Error in Wait4: %s", err.Error())
	}
	if zpid > 0 {
		n.Error("Ate a tasty zombie (pid was %d, status was %d)", zpid, w.ExitStatus())
	}

	if n.processGroupIsEmpty() {
		n.startGracePings--
		if n.startGracePings <= 0 {
			n.Error("Process group [%d] empty - exiting and hoping init sorts it all out", n.pgid)
			return true
		} else {
			n.Error("Process group [%d] empty - grace pings left [%d]", n.pgid, n.startGracePings)
		}
	} else {
		// We've had a good ping, no more Mr Nice Guy
		n.startGracePings = 0
	}
	return false
}

func (n *nelly) Finish() {
	if n.ownThePidFile {
		err := os.Remove(n.pidFileName())
		if err != nil {
			n.Error("Failed to remove pidfile [%s]: %s", n.pidFileName(), err.Error())
		}
	}
	n.App.Finish()
}

func (n *nelly) setupSignals(proc *os.Process) {
	n.sigChan = make(chan os.Signal, 10) // 10 is arbitrary, we just need to keep up
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

func (n *nelly) okToStart() bool {
	_, err := os.Stat(n.pidFileDir())
	if err != nil {
		n.Error("Can't stat pid dir [%s]: %s", n.pidFileDir(), err.Error())
		return false
	}

	pid, exists := n.readPidFile()
	if exists {
		n.Error("Pid file exists - claims pid %d owns %s:%s", pid, n.ProjectName, n.AppName)
		// Is the pid still running?
		err := syscall.Kill(pid, syscall.Signal(0x00))
		if err == nil {
			n.Error("Pid %d is running - we can't start up")
			return false
		}
		// TODO: we'd like to discriminate between ESRCH and EPERM back from Kill,
		// but I don't know how. Note that os.FindProcess is a crock of b0rkenness and can't
		// be used to...you know...find a process.
		// https://codereview.appspot.com/7392048/#msg18
		if err == nil {
			n.Error("Pid %d exists - so we have to fail startup")
			return false
		}
		n.Error("Error [%s] contacting pid %d - assume it's not there and claim pidfile", err.Error(), pid)
		// Pid file exists but proc doesn't. Continue and overwrite it with our own pid
	}
	err = n.writePidFile()
	if err != nil {
		n.Error("Can't write pid file %s: %s", n.pidFileName(), err.Error())
		return false
	}
	return true
}

func (n *nelly) writePidFile() error {
	f, err := os.OpenFile(n.pidFileName(), os.O_CREATE|os.O_RDWR, os.FileMode(0644))
	if err != nil {
		n.Error("Failed to open pid file [%s] for writing: %s", n.pidFileName(), err.Error())
		return err
	}
	defer f.Close()
	// Write our pid to the file
	f.WriteString(fmt.Sprintf("%d\n", os.Getpid()))
	n.ownThePidFile = true
	return nil
}

func (n *nelly) readPidFile() (pid int, exists bool) {
	buf, err := ioutil.ReadFile(n.pidFileName())
	if err != nil {
		if err != os.ErrNotExist {
			n.Info("Failed to read pid file: %s", err)
		}
		return 0, false
	}
	fmt.Sscanf(string(buf), "%d\n", &pid)
	if pid == 0 {
		return 0, false
	}
	return pid, true
}

func (n *nelly) pidFileDir() string {
	return fmt.Sprintf("/var/run/%s", n.ProjectName)
}

func (n *nelly) pidFileName() string {
	return fmt.Sprintf("%s/%s.pid", n.pidFileDir(), n.AppName)
}
