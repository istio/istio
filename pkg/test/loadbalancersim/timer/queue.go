//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package timer

import (
	"container/heap"
	"sync"
	"time"
)

type Queue struct {
	heap            timerHeap
	mutex           sync.Mutex
	stopCh          chan struct{}
	resetTimerCh    chan struct{}
	stopping        bool
	timer           *time.Timer
	currentDeadline time.Time
}

func NewQueue() *Queue {
	q := &Queue{
		heap:         make(timerHeap, 0),
		timer:        time.NewTimer(1 * time.Minute),
		stopCh:       make(chan struct{}),
		resetTimerCh: make(chan struct{}),
	}

	// Start the worker thread.
	go func() {
		for {
			select {
			case <-q.stopCh:
				q.stopTimer()
				return
			case <-q.resetTimerCh:
				q.resetTimer()
			case <-q.timer.C:
				q.onTimerExpired()
			}
		}
	}()

	return q
}

func (q *Queue) Len() int {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	return q.heap.Len()
}

func (q *Queue) Schedule(handler func(), deadline time.Time) {
	// Add the timer to the heap.
	q.mutex.Lock()
	heap.Push(&q.heap, &entry{
		handler:  handler,
		deadline: deadline,
		index:    0,
	})
	q.mutex.Unlock()

	// Request that the timer be reset.
	q.resetTimerCh <- struct{}{}
}

func (q *Queue) ShutDown() {
	close(q.stopCh)
}

func (q *Queue) stopTimer() {
	q.mutex.Lock()
	q.stopping = true
	q.mutex.Unlock()

	q.timer.Stop()
}

func (q *Queue) resetTimer() {
	// Below is a separate function to limit the scope of the lock.
	// We don't want to lock when we modify the timer in case it causes
	// an immediate callback, which would reacquire the lock.
	needReset, resetDuration := func() (bool, time.Duration) {
		q.mutex.Lock()
		defer q.mutex.Unlock()

		if q.stopping {
			// Ignore the event, since we're already shutting down.
			return false, 0
		}

		e := q.heap.peek()
		if e == nil || e.deadline.Equal(q.currentDeadline) {
			// nothing to do.
			return false, 0
		}

		q.currentDeadline = e.deadline
		return true, time.Until(e.deadline)
	}()

	// Reset the timer.
	if needReset {
		q.timer.Reset(resetDuration)
	}
}

func (q *Queue) onTimerExpired() {
	// Collect all expired timers.
	q.mutex.Lock()
	handlers := q.heap.advanceTo(time.Now())
	q.mutex.Unlock()

	// Call the expired timer handlers.
	for _, h := range handlers {
		h()
	}

	// Reset the timer based on the earliest deadline.
	q.resetTimer()
}

type entry struct {
	deadline time.Time
	handler  func()
	index    int
}

type timerHeap []*entry

func (h timerHeap) peek() *entry {
	if h.Len() > 0 {
		return h[0]
	}
	return nil
}

func (h *timerHeap) advanceTo(tnow time.Time) (out []func()) {
	for {
		if top := h.peek(); top != nil && !top.deadline.After(tnow) {
			heap.Remove(h, top.index)
			out = append(out, top.handler)
		} else {
			// There are no further expired timers.
			return out
		}
	}
}

func (h timerHeap) Len() int {
	return len(h)
}

func (h timerHeap) Less(i, j int) bool {
	return h[i].deadline.Before(h[j].deadline)
}

func (h timerHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *timerHeap) Push(x any) {
	e := x.(*entry)
	*h = append(*h, e)
	e.index = len(*h) - 1
}

func (h *timerHeap) Pop() any {
	n := h.Len()
	e := (*h)[n-1]
	*h = (*h)[:n-1]
	e.index = -1
	return e
}
