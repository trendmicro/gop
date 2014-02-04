package test

import (
	"fmt"
	"reflect"
	"testing"
)

func Assert(t *testing.T, assertion bool, goodMsg, badMsg string) {
	if assertion {
		t.Log("OK: ", goodMsg)
	} else {
		t.Error("NOT OK: ", badMsg)
	}
}

func OK(t *testing.T, assertion bool, msg string) {
	Assert(t, assertion, msg, msg)
}

func ErrIs(t *testing.T, got, expected error, msg string) {
	gotErr := "<nil>"
	if got != nil {
		gotErr = got.Error()
	}
	Assert(t, got == expected, msg, msg+" err: "+gotErr)
}

func ErrNotNil(t *testing.T, got error, msg string) {
	gotErr := "<nil>"
	if got != nil {
		gotErr = got.Error()
	}
	Assert(t, got != nil, msg, msg+" err: "+gotErr)
}

func Is(t *testing.T, got interface{}, expected interface{}, what string) {
	Assert(t, reflect.DeepEqual(got, expected), what+" - expected and got "+fmt.Sprint(expected), what+" got "+fmt.Sprint(got)+", expected "+fmt.Sprint(expected))
}
