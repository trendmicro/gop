package test

import (
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
	Assert(t, got == expected, msg, msg + " err: " + gotErr)
}

func ErrNotNil(t *testing.T, got error, msg string) {
	gotErr := "<nil>"
	if got != nil {
		gotErr = got.Error()
	}
	Assert(t, got != nil, msg, msg + " err: " + gotErr)
}
