package util

import (
	"fmt"
	"testing"
)

var (
	testErrs = Errors{fmt.Errorf("err1"), fmt.Errorf("err2")}
	wantStr  = "err1, err2"
)

func TestError(t *testing.T) {
	if got, want := testErrs.Error(), wantStr; got != want {
		t.Errorf("got: %s, want: %s", got, want)
	}
}

func TestString(t *testing.T) {
	if got, want := testErrs.String(), wantStr; got != want {
		t.Errorf("got: %s, want: %s", got, want)
	}
}

func TestToString(t *testing.T) {
	if got, want := ToString(testErrs), wantStr; got != want {
		t.Errorf("got: %s, want: %s", got, want)
	}
}

func TestNewErrs(t *testing.T) {
	errs := NewErrs(nil)
	if errs != nil {
		t.Errorf("got: %s, want: nil", errs)
	}

	errs = NewErrs(fmt.Errorf("err1"))
	if got, want := errs.String(), "err1"; got != want {
		t.Errorf("got: %s, want: %s", got, want)
	}
}

func TestAppendErr(t *testing.T) {
	var errs Errors
	if got, want := errs.String(), ""; got != want {
		t.Errorf("got: %s, want: %s", got, want)
	}

	errs = AppendErr(errs, nil)
	if got, want := errs.String(), ""; got != want {
		t.Errorf("got: %s, want: %s", got, want)
	}

	errs = AppendErr(errs, fmt.Errorf("err1"))
	if got, want := errs.String(), "err1"; got != want {
		t.Errorf("got: %s, want: %s", got, want)
	}

	errs = AppendErr(errs, nil)
	errs = AppendErr(errs, fmt.Errorf("err2"))
	if got, want := errs.String(), "err1, err2"; got != want {
		t.Errorf("got: %s, want: %s", got, want)
	}
}

func TestAppendErrs(t *testing.T) {
	var errs Errors

	errs = AppendErrs(errs, []error{nil})
	if got, want := errs.String(), ""; got != want {
		t.Errorf("got: %s, want: %s", got, want)
	}

	errs = AppendErrs(errs, testErrs)
	errs = AppendErrs(errs, []error{nil})
	if got, want := errs.String(), wantStr; got != want {
		t.Errorf("got: %s, want: %s", got, want)
	}
}

func TestAppendErrsInFunction(t *testing.T) {
	myAppendErrFunc := func() (errs Errors) {
		errs = AppendErr(errs, fmt.Errorf("err1"))
		errs = AppendErr(errs, fmt.Errorf("err2"))
		return
	}
	if got, want := myAppendErrFunc().String(), wantStr; got != want {
		t.Errorf("got: %s, want: %s", got, want)
	}

	myAppendErrsFunc := func() (errs Errors) {
		errs = AppendErrs(errs, testErrs)
		return
	}
	if got, want := myAppendErrsFunc().String(), wantStr; got != want {
		t.Errorf("got: %s, want: %s", got, want)
	}

	myErrorSliceFunc := func() (errs []error) {
		errs = AppendErrs(errs, testErrs)
		return
	}

	if got, want := Errors(myErrorSliceFunc()).String(), wantStr; got != want {
		t.Errorf("got: %s, want: %s", got, want)
	}
}
