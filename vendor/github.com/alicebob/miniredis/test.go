package miniredis

import (
	"reflect"
	"testing"
)

// assert fails the test if the condition is false.
func assert(tb testing.TB, condition bool, msg string, v ...interface{}) {
	tb.Helper()

	if !condition {
		tb.Errorf(msg, v...)
	}
}

// ok fails the test if an err is not nil.
func ok(tb testing.TB, err error) {
	tb.Helper()

	if err != nil {
		tb.Errorf("unexpected error: %s", err.Error())
	}
}

// equals fails the test if exp is not equal to act.
func equals(tb testing.TB, exp, act interface{}) {
	tb.Helper()

	if !reflect.DeepEqual(exp, act) {
		tb.Errorf("expected: %#v got: %#v", exp, act)
	}
}

// mustFail compares the error strings
func mustFail(tb testing.TB, err error, want string) {
	tb.Helper()

	if err == nil {
		tb.Errorf("expected an error, but got a nil")
	}

	if have := err.Error(); have != want {
		tb.Errorf("have %q, want %q", have, want)
	}
}
