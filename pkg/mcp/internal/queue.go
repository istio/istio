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

package internal

import (
	"encoding/json"
	"sync"
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
// - Enqueuing an item in the queue creates a trySchedule event. The caller can select
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
	mu             sync.Mutex
	doneChan       chan struct{}
	doneChanClosed bool
	// Enqueue at the tail, Dequeue from the head.
	// head == tail   -> empty
	// head == tail+1 -> full
	head int
	tail int
	// Maintain an ordered set of enqueued items.
	queue     []entry
	queuedSet map[string]*entry
	maxDepth  int

	readyChan chan struct{}
}

type entry struct {
	key string
	val interface{}
}

// NewUniqueScheduledQueue creates a new unique queue specialized for MCP source/server implementations.
func NewUniqueScheduledQueue(maxDepth int) *UniqueQueue {
	return &UniqueQueue{
		// Max queue size is one larger than the max depth so that
		// we can differentiate empty vs. full conditions.
		//
		// 	head == tail   -> empty
		// 	head == tail+1 -> full
		queue: make([]entry, maxDepth+1),

		queuedSet: make(map[string]*entry, maxDepth),
		maxDepth:  maxDepth,
		readyChan: make(chan struct{}, maxDepth),
		doneChan:  make(chan struct{}),
	}
}

// return the next index accounting for wrap
func (q *UniqueQueue) inc(idx int) int {
	return (idx + 1) % (q.maxDepth + 1)
}

// Empty returns true if the queue is empty
func (q *UniqueQueue) Empty() bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	// 	head == tail -> empty
	return q.head == q.tail
}

// internal version of full() that can be called
// with the lock held.
func (q *UniqueQueue) full() bool {
	// 	head == tail+1 -> full
	return q.head == q.inc(q.tail)
}

// Full returns true if the queue is full
func (q *UniqueQueue) Full() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.full()
}

// Enqueue an item in the queue. Items with the same key may be safely enqueued multiple
// times. Enqueueing an item with a key that has already queued has no affect on the order
// of existing queued items.
//
// Returns true if the item exists in the queue upon return. Otherwise,
// returns false if the item could not be queued.
func (q *UniqueQueue) Enqueue(key string, val interface{}) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.doneChanClosed {
		return false
	}

	// Key is already in the queue. Update the set's entry and
	// return without modifying its place in the queue.
	if entry, ok := q.queuedSet[key]; ok {
		entry.val = val
		return true
	}

	if q.full() {
		return false
	}

	q.queue[q.tail] = entry{
		key: key,
		val: val,
	}
	q.queuedSet[key] = &q.queue[q.tail]
	q.tail = q.inc(q.tail)

	q.trySchedule()

	return true
}

// must be called with lock held
func (q *UniqueQueue) trySchedule() {
	select {
	case q.readyChan <- struct{}{}:
	default:
		scope.Warnf("queue could not be scheduled (head=%v tail=%v depth=%v)",
			q.head, q.tail, q.maxDepth)
	}
}

// Dequeue removes an item from the queue. This should only be called once for each
// time Ready() indicates a new item is ready to be dequeued.
func (q *UniqueQueue) Dequeue() (string, interface{}, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.head == q.tail {
		return "", nil, false
	}

	entry := q.queue[q.head]
	q.head = q.inc(q.head)
	delete(q.queuedSet, entry.key)
	return entry.key, entry.val, true
}

func (q *UniqueQueue) Ready() <-chan struct{} {
	return q.readyChan
}

func (q *UniqueQueue) Done() <-chan struct{} {
	return q.doneChan
}

func (q *UniqueQueue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.doneChanClosed {
		q.doneChanClosed = true
		close(q.doneChan)
	}
}

type dump struct {
	Closed    bool                   `json:"closed"`
	QueuedSet map[string]interface{} `json:"queued_set"`
	Queue     []string               `json:"queue"`
	Head      int                    `json:"head"`
	Tail      int                    `json:"tail"`
	MaxDepth  int                    `json:"max_depth"`
}

// hook for unit tests
var jsonMarshalDumpHook = json.Marshal

// Dump returns a JSON formatted dump of the internal queue state. This is intended
// for debug purposes only.
func (q *UniqueQueue) Dump() string {
	q.mu.Lock()
	defer q.mu.Unlock()

	d := &dump{
		Closed:    q.doneChanClosed,
		Head:      q.head,
		Tail:      q.tail,
		MaxDepth:  q.maxDepth,
		Queue:     make([]string, 0, len(q.queue)),
		QueuedSet: make(map[string]interface{}, len(q.queuedSet)),
	}

	for _, entry := range q.queue {
		d.Queue = append(d.Queue, entry.key)
	}
	for _, entry := range q.queuedSet {
		d.QueuedSet[entry.key] = entry.val
	}

	out, err := jsonMarshalDumpHook(d)
	if err != nil {
		return ""
	}
	return string(out)
}
