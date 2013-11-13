package test

import (
	"testing"
)

func OK(t *testing.T, assertion bool, msg string) {
	if assertion {
		t.Log("OK: ", msg)
	} else {
		t.Error("OK: ", msg)
	}
}
