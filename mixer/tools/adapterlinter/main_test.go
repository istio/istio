// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"reflect"
	"sort"
	"testing"
)

func TestDoAllDirs(t *testing.T) {
	reps := doAllDirs([]string{"testdata/bad"})

	got := make([]string, len(reps))
	for i := range reps {
		got[i] = reps[i].msg
	}

	sort.Strings(got)
	want := []string{
		"testdata/bad/gorutn_logimprt.gen.go:14:2:Adapters must use env.ScheduleWork or env.ScheduleDaemon " +
			"in order to dispatch goroutines. This ensures all adapter goroutines are prevented from crashing Mixer " +
			"as a whole by catching any panics they produce.",
		"testdata/bad/gorutn_logimprt.gen.go:5:2:\"log\" import is not recommended; Adapters must instead use " +
			"env.Logger for logging during execution. This logger understands which adapter is running and routes " +
			"the data to the place where the operator wants to see it.",
		"testdata/bad/gorutn_logimprt2.gen.go:15:2:Adapters must use env.ScheduleWork or env.ScheduleDaemon " +
			"in order to dispatch goroutines. This ensures all adapter goroutines are prevented from crashing Mixer " +
			"as a whole by catching any panics they produce.",
		"testdata/bad/gorutn_logimprt2.gen.go:6:2:\"github.com/golang/glog\" import is not recommended; Adapters must instead use " +
			"env.Logger for logging during execution. This logger understands which adapter is running and routes " +
			"the data to the place where the operator wants to see it.",
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("errors don't match\nwant:%v\ngot :%v", want, got)
	}
}

func TestDoAllDirsBadPath(t *testing.T) {
	// check no panics and no reports
	got := getReport([]string{"testdata/unknown"})
	want := reports{}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("errors don't match\nwant:%v\ngot :%v", want, got)
	}
}

func TestDoAllDirsGood(t *testing.T) {
	got := getReport([]string{"testdata/bad2"})
	want := reports{}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("errors don't match\nwant:%v\ngot :%v", want, got)
	}
}

func TestDoAllDirsCurrentDir(t *testing.T) {
	got := getReport([]string{})
	want := reports{}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("errors don't match\nwant:%v\ngot :%v", want, got)
	}
}

func TestReportSort(t *testing.T) {
	rpts := reports([]report{{pos: 2}, {pos: 1}, {pos: 4}, {pos: 3}})
	sort.Sort(rpts)
	want := reports{report{pos: 1}, report{pos: 2}, report{pos: 3}, report{pos: 4}}
	if !reflect.DeepEqual(want, rpts) {
		t.Errorf("errors don't match\nwant:%v\ngot :%v", want, rpts)
	}
}
