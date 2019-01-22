// Copyright 2018 Istio Authors
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

package internal

import (
	"sync"
	"time"

	"istio.io/istio/pkg/mcp/env"
)

// TODO - this can eventually be moved under pkg/mcp/source once the new stack is
// introduced. The code is temporarily added under pkg/mcp/internal so
// we can share across package boundaries without exposing outside of the pkg/mcp
// parent directory.

// UniqueQueue is a specialized queue structure. It has the following properties:
//
// - Enqueuing an item already in the queue is idempotent. The first time an item
//   is added to the queue determines its order in the queue until it is dequeued.
//   Callers can safely update the queued items state while preserving it's place
//   in the queue.
//
// - Enqueuing an item in the queue creates a schedule event. The caller can select
//   over the Ready() channel to process this event and remove items from the queue.
//
// - The maximum queue depth is fixed.
//
// - The queue can be safely closed. The caller is responsible for checking the Done()
//   state of the queue and should stop checking Ready() and invoking Dequeue() when
//   the channel returned by Done() is closed.
//
// This is intended to be used by the MCP source/server packages for managing
// per-type watch state.
type UniqueQueue struct {
	sync.Mutex
	doneChan       chan struct{}
	doneChanClosed bool
	queuedSet      map[interface{}]struct{}
	// Enqueue at the tail, Dequeue from the head
	// head == tail   => empty
	// head == tail+1 => full
	head     int
	tail     int
	queue    []interface{}
	maxDepth int

	scheduleChan chan struct{}
}

// NewUniqueScheduledQueue creates a new unique queue specialized for MCP source/server implementations.
func NewUniqueScheduledQueue(maxDepth int) *UniqueQueue {
	return &UniqueQueue{
		queue:        make([]interface{}, maxDepth+1),
		queuedSet:    make(map[interface{}]struct{}, maxDepth),
		maxDepth:     maxDepth,
		scheduleChan: make(chan struct{}, maxDepth),
		doneChan:     make(chan struct{}),
	}
}

// return the next index accounting for wrap
func (q *UniqueQueue) inc(idx int) int {
	idx = idx + 1
	if idx == q.maxDepth+1 {
		idx = 0
	}
	return idx
}

// Empty returns true if the queue is empty
func (q *UniqueQueue) Empty() bool {
	q.Lock()
	defer q.Unlock()
	return q.head == q.tail
}

// internal version of full() that can be called
// with the lock held.
func (q *UniqueQueue) full() bool {
	return q.head == q.inc(q.tail)
}

// Full returns true if the queue is full
func (q *UniqueQueue) Full() bool {
	q.Lock()
	defer q.Unlock()
	return q.full()
}

// Enqueue an item in the queue. The same item may be safely enqueued multiple
// times. Attempts to enqueue an already queued item have no affect on the order
// of already queued items.
//
// Returns true if the item exists in the queue upon return. Otherwise,
// returns false if the item could not be queued.
func (q *UniqueQueue) Enqueue(w interface{}) bool {
	q.Lock()
	defer q.Unlock()

	// already in the queued set of items
	if _, ok := q.queuedSet[w]; ok {
		return true
	}

	if q.full() {
		return false
	}

	q.queuedSet[w] = struct{}{}
	q.queue[q.tail] = w
	q.tail = q.inc(q.tail)

	q.Schedule()
	return true
}

func (q *UniqueQueue) Dequeue() interface{} {
	q.Lock()
	defer q.Unlock()

	if q.head == q.tail {
		return nil
	}

	w := q.queue[q.head]
	q.head = q.inc(q.head)
	delete(q.queuedSet, w)
	return w
}

func (q *UniqueQueue) Ready() <-chan struct{} {
	return q.scheduleChan
}

func (q *UniqueQueue) Done() <-chan struct{} {
	return q.doneChan
}

// Controls the delay for re-retrying a configuration push if the previous
// attempt was not possible, e.g. the lower-level serving layer was busy. This
// should typically be set fairly small (order of milliseconds).
var retryPushDelay = env.Duration("RETRY_PUSH_DELAY", 10*time.Millisecond)

func (q *UniqueQueue) Schedule() {
	select {
	case q.scheduleChan <- struct{}{}:
	default:
		// retry
		time.AfterFunc(retryPushDelay, q.Schedule)
	}
}

//
func (q *UniqueQueue) Close() {
	q.Lock()
	defer q.Unlock()

	if !q.doneChanClosed {
		q.doneChanClosed = true
		close(q.doneChan)
	}
}
