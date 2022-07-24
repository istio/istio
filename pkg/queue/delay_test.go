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

package queue

import (
	"container/heap"
	"sync"
	"testing"
	"time"
)

func TestPriorityQueue(t *testing.T) {
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

func TestDelayQueueOrdering(t *testing.T) {
	dq := NewDelayed(DelayQueueWorkers(2))
	stop := make(chan struct{})
	defer close(stop)
	go dq.Run(stop)

	mu := sync.Mutex{}
	var t0, t1, t2 time.Time

	done := make(chan struct{})
	dq.PushDelayed(func() error {
		mu.Lock()
		defer mu.Unlock()
		defer close(done)
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

	select {
	case <-time.After(500 * time.Millisecond):
	case <-done:
	}

	mu.Lock()
	if !(t2.After(t1) && t1.After(t0)) {
		t.Errorf("expected jobs to be run in order based on delays")
	}
	mu.Unlock()
}

func TestDelayQueuePushBeforeRun(t *testing.T) {
	// This is a regression test to ensure we can push while Run() is called without a race
	dq := NewDelayed(DelayQueueBuffer(0))
	st := make(chan struct{})
	go func() {
		// Enqueue a bunch until we stop
		for {
			select {
			case <-st:
				return
			default:
			}
			dq.Push(func() error {
				return nil
			})
		}
	}()
	go dq.Run(st)
	// Wait a bit
	<-time.After(time.Millisecond * 10)
	close(st)
}

func TestDelayQueuePushNonblockingWithFullBuffer(t *testing.T) {
	queuedItems := 50
	dq := NewDelayed(DelayQueueBuffer(0), DelayQueueWorkers(0))

	success := make(chan struct{})
	timeout := time.After(500 * time.Millisecond)
	defer close(success)

	go func() {
		for i := 0; i < queuedItems; i++ {
			dq.PushDelayed(func() error { return nil }, time.Minute*time.Duration(queuedItems-i))
		}
		success <- struct{}{}
	}()

	select {
	case <-success:
		dq := dq.(*delayQueue)
		dq.mu.Lock()
		if dq.queue.Len() < queuedItems {
			t.Fatalf("expected 50 items in the queue, got %d", dq.queue.Len())
		}
		dq.mu.Unlock()
		return
	case <-timeout:
		t.Fatal("timed out waiting for enqueues")
	}
}

func TestPriorityQueueShrinking(t *testing.T) {
	c := 48
	pq := make(pq, 0, c)
	pqp := &pq

	t0 := time.Now()
	for i := 0; i < c; i++ {
		dt := &delayTask{runAt: t0.Add(time.Duration(i) * time.Hour)}
		heap.Push(pqp, dt)
	}

	if len(pq) != c {
		t.Fatalf("the length of pq should be %d, but end up %d", c, len(pq))
	}

	if cap(pq) != c {
		t.Fatalf("the capacity of pq should be %d, but end up %d", c, cap(pq))
	}

	for i := 0; i < c; i++ {
		_ = heap.Pop(pqp)
		if i == 1+c/2 && cap(pq) != c/2 {
			t.Fatalf("the capacity of pq should be reduced to half its length %d, but got %d", c/2, cap(pq))
		}
	}
}
