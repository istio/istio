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

package queue

import (
	"container/heap"
	"sync"
	"testing"
	"time"
)

// TODO benchmarks

func TestPq(t *testing.T) {
	pq := &pq{}

	t0 := time.Now()
	t1 := &delayTask{runAt: t0.Add(0)}
	t2 := &delayTask{runAt: t0.Add(1 * time.Hour)}
	t3 := &delayTask{runAt: t0.Add(2 * time.Hour)}
	t4 := &delayTask{runAt: t0.Add(3 * time.Hour)}
	sorted := []*delayTask{t1, t2, t3, t4}
	// fill in an unsorted order
	unsorted := []*delayTask{t4, t2, t3, t1}
	for _, task := range unsorted {
		heap.Push(pq, task)
	}

	// dequeue should be in order
	for i, task := range sorted {
		peeked := pq.Peek()
		popped := heap.Pop(pq)
		if task != popped {
			t.Fatalf("pop %d was not in order", i)
		}
		if peeked != popped {
			t.Fatalf("did not peek at the next item to be popped")
		}
	}
}

func TestDelayQueue(t *testing.T) {
	dq := NewDelayed(2)
	stop := make(chan struct{})
	defer close(stop)
	go dq.Run(stop)

	mu := sync.Mutex{}
	var t0, t1, t2 time.Time

	dq.PushDelayed(func() error {
		mu.Lock()
		defer mu.Unlock()
		t2 = time.Now()
		return nil
	}, 200*time.Millisecond)
	dq.PushDelayed(func() error {
		mu.Lock()
		defer mu.Unlock()
		t1 = time.Now()
		return nil
	}, 100*time.Millisecond)
	dq.Push(func() error {
		mu.Lock()
		defer mu.Unlock()
		t0 = time.Now()
		return nil
	})
	// TODO don't do this
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	if !(t2.After(t1) && t1.After(t0)) {
		t.Errorf("expected jobs to be run in order based on delays")
	}
	mu.Unlock()
}
