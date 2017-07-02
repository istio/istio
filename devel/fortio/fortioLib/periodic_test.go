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
	"testing"
	"time"
)

func noop() {
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
		// Error cases -> 1 qps
		{qps: -10, numThreads: 0, expectedQPS: 1, expectedNumThreads: 1},
		{qps: 0, numThreads: -6, expectedQPS: 1, expectedNumThreads: 1},
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

func TestStart(t *testing.T) {
	r := NewPeriodicRunner(11.4, noop)
	r.SetDebug(true)
	r.Run(1 * time.Second)
}
