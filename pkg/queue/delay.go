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
	"time"

	"istio.io/pkg/log"
)

type delayTask struct {
	do    func() error
	runAt time.Time
}

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

func (q *pq) Push(x interface{}) {
	*q = append(*q, x.(*delayTask))
}

func (q *pq) Pop() interface{} {
	old := *q
	n := len(old)
	if n == 0 {
		return nil
	}
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	*q = old[0 : n-1]
	return item
}

// Peek is not managed by the container/heap package, so we return the 0th element in the list.
func (q *pq) Peek() interface{} {
	if q.Len() < 1 {
		return nil
	}
	return (*q)[0]
}

// Delayed ipmlements queue such that tasks are executed after a specified delay.
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
		// TODO buffer execute channel?
	}
}

// NewDelayed gives a Delayed queue with maximum concurrency specified by workers.
func NewDelayed(opts ...DelayQueueOption) Delayed {
	q := &delayQueue{
		workers: 1,
		queue:   &pq{},
		execute: make(chan *delayTask),
		enqueue: make(chan *delayTask, 100),
	}
	for _, o := range opts {
		o(q)
	}
	return q
}

type delayQueue struct {
	workers int
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

func (d *delayQueue) Run(stop <-chan struct{}) {
	for i := 0; i < d.workers; i++ {
		go d.work(stop)
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
				d.execute <- task
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
					d.execute <- task
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
				d.queue.Push(t)
			case <-stop:
				return
			}
		}
	}
}

func (d *delayQueue) work(stop <-chan struct{}) {
	for {
		select {
		case t := <-d.execute:
			if err := t.do(); err != nil {
				log.Errorf("Work item handle failed: %v", err)
				// TODO requeue?
			}
		case <-stop:
			return
		}
	}
}
