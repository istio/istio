package main

import (
	"reflect"
	"testing"
)

func TestDoAllDirs(t *testing.T) {
	got := doAllDirs([]string{"testdata/bad"})

	want := []string{
		"testdata/bad/gorutn_logimprt.go:5:2:\"log\" import is not recommended; Adapters must instead use " +
			"env.Logger for logging during execution. This logger understands about which adapter is running and routes " +
			"the data to the place where the operator wants to see it.",
		"testdata/bad/gorutn_logimprt.go:14:2:Adapters must use env.ScheduleWork or env.ScheduleDaemon " +
			"in order to dispatch goroutines. This ensures all adapter goroutines are prevented from crashing Mixer " +
			"as a whole by catching any panics they produce.",
		"testdata/bad/gorutn_logimprt2.go:6:2:\"github.com/golang/glog\" import is not recommended; Adapters must instead use " +
			"env.Logger for logging during execution. This logger understands about which adapter is running and routes " +
			"the data to the place where the operator wants to see it.",
		"testdata/bad/gorutn_logimprt2.go:15:2:Adapters must use env.ScheduleWork or env.ScheduleDaemon " +
			"in order to dispatch goroutines. This ensures all adapter goroutines are prevented from crashing Mixer " +
			"as a whole by catching any panics they produce.",
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("errors dont match\nwant:%v\ngot :%v", want, got)
	}
}

func TestDoAllDirsBadPath(t *testing.T) {
	// check no panics and no reports
	got := doAllDirs([]string{"testdata/unknown"})
	want := []string{}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("errors dont match\nwant:%v\ngot :%v", want, got)
	}
}

func TestDoAllDirsGood(t *testing.T) {
	got := doAllDirs([]string{"testdata/good"})
	want := []string{}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("errors dont match\nwant:%v\ngot :%v", want, got)
	}
}
