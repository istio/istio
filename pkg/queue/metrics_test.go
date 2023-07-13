// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package queue

import (
	"sync"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/features"
)

func BenchmarkMetricsQueue(b *testing.B) {
	features.EnableControllerQueueMetrics = true
	q := NewQueue(1 * time.Microsecond)
	s := make(chan struct{})
	go q.Run(s)
	for n := 0; n < b.N; n++ {
		wg := sync.WaitGroup{}
		wg.Add(1)
		q.Push(func() error {
			wg.Done()
			return nil
		})
		wg.Wait()

	}
	close(s)
}

func BenchmarkMetricsQueueDisabled(b *testing.B) {
	features.EnableControllerQueueMetrics = false
	q := NewQueue(1 * time.Microsecond)
	s := make(chan struct{})
	go q.Run(s)
	for n := 0; n < b.N; n++ {
		wg := sync.WaitGroup{}
		wg.Add(1)
		q.Push(func() error {
			wg.Done()
			return nil
		})
		wg.Wait()
	}
	close(s)
}

func BenchmarkMetricsQueueInc(b *testing.B) {
	q := newQueueMetrics("test")
	for n := 0; n < b.N; n++ {
		q.depth.Increment()
	}
}

func BenchmarkMetricsQueueRec(b *testing.B) {
	q := newQueueMetrics("test")
	for n := 0; n < b.N; n++ {
		q.depth.Record(100)
	}
}

func BenchmarkMetricsQueueSinceInSeconds(b *testing.B) {
	q := newQueueMetrics("test")
	dt := time.Now()
	for n := 0; n < b.N; n++ {
		q.sinceInSeconds(dt)
	}
}
