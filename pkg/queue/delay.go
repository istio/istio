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
	"runtime"
	"sync"
	"time"

	"istio.io/pkg/log"
)

type delayTask struct {
	do      func() error
	runAt   time.Time
	retries int
}

const maxTaskRetry = 3

var _ heap.Interface = &pq{}

// pq implements an internal priority queue so that tasks with the soonest expiry will be run first.
// Methods on pq are not threadsafe, access should be protected.
// much of this is taken from the example at https://golang.org/pkg/container/heap/
type pq []*delayTask

func (q pq) Len() int {
	return len(q)
}

func (q pq) Less(i, j int) bool {
	return q[i].runAt.Before(q[j].runAt)
}

func (q *pq) Swap(i, j int) {
	(*q)[i], (*q)[j] = (*q)[j], (*q)[i]
}

func (q *pq) Push(x any) {
	*q = append(*q, x.(*delayTask))
}

func (q *pq) Pop() any {
	old := *q
	n := len(old)
	c := cap(old)
	// Shrink the capacity of task queue.
	if n < c/2 && c > 32 {
		npq := make(pq, n, c/2)
		copy(npq, old)
		old = npq
	}
	if n == 0 {
		return nil
	}
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	*q = old[0 : n-1]
	return item
}

// Peek is not managed by the container/heap package, so we return the 0th element in the list.
func (q *pq) Peek() any {
	if q.Len() < 1 {
		return nil
	}
	return (*q)[0]
}

// Delayed implements queue such that tasks are executed after a specified delay.
type Delayed interface {
	Instance
	PushDelayed(t Task, delay time.Duration)
}

var _ Delayed = &delayQueue{}

// DelayQueueOption configure the behavior of the queue. Must be applied before Run.
type DelayQueueOption func(*delayQueue)

// DelayQueueBuffer sets maximum number of tasks awaiting execution. If this limit is reached, Push and PushDelayed
// will block until there is room.
func DelayQueueBuffer(bufferSize int) DelayQueueOption {
	return func(queue *delayQueue) {
		if queue.enqueue != nil {
			close(queue.enqueue)
		}
		queue.enqueue = make(chan *delayTask, bufferSize)
	}
}

// DelayQueueWorkers sets the number of background worker goroutines await tasks to execute. Effectively the
// maximum number of concurrent tasks.
func DelayQueueWorkers(workers int) DelayQueueOption {
	return func(queue *delayQueue) {
		queue.workers = workers
	}
}

// workerChanBuf determines whether the channel of a worker should be a buffered channel
// to get the best performance.
var workerChanBuf = func() int {
	// Use blocking channel if GOMAXPROCS=1.
	// This switches context from sender to receiver immediately,
	// which results in higher performance.
	var n int
	if n = runtime.GOMAXPROCS(0); n == 1 {
		return 0
	}

	// Make channel non-blocking and set up its capacity with GOMAXPROCS if GOMAXPROCS>1,
	// otherwise the sender might be dragged down if the receiver is CPU-bound.
	//
	// GOMAXPROCS determines how many goroutines can run in parallel,
	// which makes it the best choice as the channel capacity,
	return n
}()

// NewDelayed gives a Delayed queue with maximum concurrency specified by workers.
func NewDelayed(opts ...DelayQueueOption) Delayed {
	q := &delayQueue{
		workers: 1,
		queue:   &pq{},
		execute: make(chan *delayTask, workerChanBuf),
		enqueue: make(chan *delayTask, 100),
	}
	for _, o := range opts {
		o(q)
	}
	return q
}

type delayQueue struct {
	workers       int
	workerStopped []chan struct{}

	// incoming
	enqueue chan *delayTask
	// outgoing
	execute chan *delayTask

	mu    sync.Mutex
	queue *pq
}

// PushDelayed will execute the task after waiting for the delay
func (d *delayQueue) PushDelayed(t Task, delay time.Duration) {
	task := &delayTask{do: t, runAt: time.Now().Add(delay)}
	select {
	case d.enqueue <- task:
	// buffer has room to enqueue
	default:
		// TODO warn and resize buffer
		// if the buffer is full, we take the more expensive route of locking and pushing directly to the heap
		d.mu.Lock()
		heap.Push(d.queue, task)
		d.mu.Unlock()
	}
}

// Push will execute the task as soon as possible
func (d *delayQueue) Push(task Task) {
	d.PushDelayed(task, 0)
}

func (d *delayQueue) Closed() <-chan struct{} {
	done := make(chan struct{})
	go func() {
		for _, ch := range d.workerStopped {
			<-ch
		}
		close(done)
	}()
	return done
}

func (d *delayQueue) Run(stop <-chan struct{}) {
	for i := 0; i < d.workers; i++ {
		d.workerStopped = append(d.workerStopped, d.work(stop))
	}

	push := func(t *delayTask) bool {
		select {
		case d.execute <- t:
			return true
		case <-stop:
			return false
		}
	}

	for {
		var task *delayTask
		d.mu.Lock()
		if head := d.queue.Peek(); head != nil {
			task = head.(*delayTask)
			heap.Pop(d.queue)
		}
		d.mu.Unlock()

		if task != nil {
			delay := time.Until(task.runAt)
			if delay <= 0 {
				// execute now and continue processing incoming enqueues/tasks
				if !push(task) {
					return
				}
			} else {
				// not ready yet, don't block enqueueing
				await := time.NewTimer(delay)
				select {
				case t := <-d.enqueue:
					d.mu.Lock()
					heap.Push(d.queue, t)
					// put the old "head" back on the queue, it may be scheduled to execute after the one
					// that was just pushed
					heap.Push(d.queue, task)
					d.mu.Unlock()
				case <-await.C:
					if !push(task) {
						return
					}
				case <-stop:
					await.Stop()
					return
				}
				await.Stop()
			}
		} else {
			// no items, wait for Push or stop
			select {
			case t := <-d.enqueue:
				d.mu.Lock()
				d.queue.Push(t)
				d.mu.Unlock()
			case <-stop:
				return
			}
		}
	}
}

// work takes a channel that signals to stop, and returns a channel that signals the worker has fully stopped
func (d *delayQueue) work(stop <-chan struct{}) (stopped chan struct{}) {
	stopped = make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case t := <-d.execute:
				if err := t.do(); err != nil {
					if t.retries < maxTaskRetry {
						d.Push(t.do)
						t.retries++
						log.Warnf("Work item handle failed: %v %d times, retry it", err, t.retries)
						continue
					}
					log.Errorf("Work item handle failed: %v, reaching the maximum retry times: %d, drop it", err, maxTaskRetry)
				}
			case <-stop:
				return
			}
		}
	}()
	return
}
