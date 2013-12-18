package gop

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

func runAsUserName(desiredUserName string) bool {
	// We do not have logging set up yet. We just panic() on error.

	if desiredUserName == "" {
		return false
	}

	currentUser, err := user.Current()
	if err != nil {
		panic(fmt.Sprintf("Can't find current user: %s", err.Error()))
	}

	desiredUser, err := user.Lookup(desiredUserName)
	if err != nil {
		// Not a fatal error, we'll just try the next
		return false
	}

	if currentUser.Uid != desiredUser.Uid {
		numericId, err := strconv.Atoi(desiredUser.Uid)
		if err != nil {
			panic(fmt.Sprintf("Can't interpret [%s] as a numeric user id [following lookup of usernmae %s]", desiredUser.Uid, desiredUserName))
		}
		err = syscall.Setuid(numericId)
		if err != nil {
			panic(fmt.Sprintf("Can't setuid to [%s]: %s", desiredUser.Uid, err.Error()))
		}
	}
	return true
}

func fdsInUse() (int64, error) {
	// Linux-specific hack
	fdDir := fmt.Sprintf("/proc/%d/fd", os.Getpid())
	fileinfo, err := ioutil.ReadDir(fdDir)
	if err != nil {
		return 0, err
	}
	return int64(len(fileinfo)), nil
}
