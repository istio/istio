// Copyright 2017 Istio Authors
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

package fortio

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

func noop(t int) {
}

func TestNewPeriodicRunner(t *testing.T) {
	var tests = []struct {
		qps                float64 // input
		numThreads         int     // input
		expectedQPS        float64 // expected
		expectedNumThreads int     // expected
	}{
		{qps: 0.1, numThreads: 1, expectedQPS: 0.1, expectedNumThreads: 1},
		{qps: 1, numThreads: 3, expectedQPS: 1, expectedNumThreads: 3},
		{qps: 10, numThreads: 10, expectedQPS: 10, expectedNumThreads: 10},
		{qps: 100000, numThreads: 10, expectedQPS: 100000, expectedNumThreads: 10},
		{qps: 0.5, numThreads: 1, expectedQPS: 0.5, expectedNumThreads: 1},
		// Error cases negative qps same as 0 qps == max speed
		{qps: -10, numThreads: 0, expectedQPS: 0, expectedNumThreads: 4},
		// Need at least 1 thread
		{qps: 0, numThreads: -6, expectedQPS: 0, expectedNumThreads: 1},
	}
	for _, tst := range tests {
		o := RunnerOptions{
			QPS:        tst.qps,
			Function:   noop,
			NumThreads: tst.numThreads,
		}
		r := newPeriodicRunner(&o)
		if r.QPS != tst.expectedQPS {
			t.Errorf("qps: got %f, not as expected %f", r.QPS, tst.expectedQPS)
		}
		if r.NumThreads != tst.expectedNumThreads {
			t.Errorf("threads: with %d input got %d, not as expected %d",
				tst.numThreads, r.NumThreads, tst.expectedNumThreads)
		}
	}
}

var lock sync.Mutex

func sumTest(count *int64) {
	lock.Lock()
	(*count)++
	lock.Unlock()
	time.Sleep(50 * time.Millisecond)
}

func TestStart(t *testing.T) {
	var count int64
	localF := func(t int) {
		sumTest(&count)
	}
	o := RunnerOptions{
		QPS:        11.4,
		Function:   localF,
		NumThreads: 1,
		Duration:   1 * time.Second,
	}
	r := NewPeriodicRunner(&o)
	count = 0
	r.Run()
	if count != 11 {
		t.Errorf("Test executed unexpected number of times %d instead %d", count, 11)
	}
	count = 0
	oo := r.Options()
	oo.NumThreads = 10 // will be lowered to 5 so 10 calls (2 in each thread)
	r.Run()
	if count != 10 {
		t.Errorf("MT Test executed unexpected number of times %d instead %d", count, 10)
	}
	// note: it's kind of a bug this only works after Run() and not before
	if oo.NumThreads != 5 {
		t.Errorf("Lowering of thread count broken, got %d instead of 5", oo.NumThreads)
	}
	count = 0
	oo.Duration = 1 * time.Nanosecond
	r.Run()
	if count != 2 {
		t.Errorf("Test executed unexpected number of times %d instead minimum 2", count)
	}
}

func TestStartMaxQps(t *testing.T) {
	var count int64
	localF := func(t int) {
		sumTest(&count)
	}
	o := RunnerOptions{
		QPS:        0,      // max speed
		Function:   localF, // 1ms sleep
		NumThreads: 4,
		Duration:   140 * time.Millisecond,
	}
	r := NewPeriodicRunner(&o)
	count = 0
	r.Run()
	expected := int64(3 * 4) // can start 3 50ms in 140ms * 4 threads
	if count != expected {
		t.Errorf("MaxQpsTest executed unexpected number of times %d instead %d", count, expected)
	}
}

func TestParsePercentiles(t *testing.T) {
	var tests = []struct {
		str  string    // input
		list []float64 // expected
		err  bool
	}{
		// Good cases
		{str: "99.9", list: []float64{99.9}},
		{str: "1,2,3", list: []float64{1, 2, 3}},
		{str: "   17, -5.3,  78  ", list: []float64{17, -5.3, 78}},
		// Errors
		{str: "", list: []float64{}, err: true},
		{str: "   ", list: []float64{}, err: true},
		{str: "23,a,46", list: []float64{23}, err: true},
	}
	SetLogLevel(Debug) // for coverage
	for _, tst := range tests {
		actual, err := ParsePercentiles(tst.str)
		if !reflect.DeepEqual(actual, tst.list) {
			t.Errorf("ParsePercentiles got %#v expected %#v", actual, tst.list)
		}
		if (err != nil) != tst.err {
			t.Errorf("ParsePercentiles got %v error while expecting err:%v for %s",
				err, tst.err, tst.str)
		}
	}
}
