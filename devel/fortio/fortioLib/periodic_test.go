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
	"sync"
	"testing"
	"time"
)

func noop(t int) {
	time.Sleep(1 * time.Millisecond)
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
		{qps: -10, numThreads: 0, expectedQPS: 0, expectedNumThreads: 1},
		// Need at least 1 thread
		{qps: 0, numThreads: -6, expectedQPS: 0, expectedNumThreads: 1},
	}
	for _, tst := range tests {
		r := newPeriodicRunner(tst.qps, noop)
		if r.qps != tst.expectedQPS {
			t.Errorf("qps: got %f, not as expected %f", r.qps, tst.expectedQPS)
		}
		r.SetNumThreads(tst.numThreads)
		if r.numThreads != tst.expectedNumThreads {
			t.Errorf("threads: got %d, not as expected %d", r.numThreads, tst.expectedNumThreads)
		}
	}
}

var count int64

var lock sync.Mutex

func sumTest(t int) {
	lock.Lock()
	count++
	lock.Unlock()

}

func TestStart(t *testing.T) {
	r := NewPeriodicRunner(11.4, sumTest)
	r.SetNumThreads(1)
	r.SetDebugLevel(1)
	count = 0
	r.Run(1 * time.Second)
	if count != 11 {
		t.Errorf("Test executed unexpected number of times %d instead %d", count, 11)
	}
	count = 0
	r.SetNumThreads(10) // will be lowered to 5 so 10 calls (2 in each thread)
	r.Run(1 * time.Second)
	if count != 10 {
		t.Errorf("MT Test executed unexpected number of times %d instead %d", count, 10)
	}
	// note: it's kind of a bug this only works after Run() and not before
	if r.GetNumThreads() != 5 {
		t.Errorf("Lowering of thread count broken, got %d instead of 5", r.GetNumThreads())
	}
	count = 0
	r.Run(1 * time.Nanosecond)
	if count != 2 {
		t.Errorf("Test executed unexpected number of times %d instead minimum 2", count)
	}
}
