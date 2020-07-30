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
	"sync"
	"time"

	"istio.io/pkg/log"
)

// Task to be performed.
type Task func() error

// Instance of work tickets processed using a rate-limiting loop
type Instance interface {
	// Push a task.
	Push(task Task)
	// Run the loop until a signal on the channel
	Run(<-chan struct{})
}

type queueImpl struct {
	delay time.Duration
	tasks []Task
	cond  *sync.Cond
	// closing indicates whether the queue is being closed
	closing bool

	mutex sync.RWMutex
	// closed indicates whether the queue is not run or closed
	closed bool
}

// NewQueue instantiates a queue with a processing function
func NewQueue(errorDelay time.Duration) Instance {
	return &queueImpl{
		delay:   errorDelay,
		tasks:   make([]Task, 0),
		closing: false,
		closed:  true,
		cond:    sync.NewCond(&sync.Mutex{}),
	}
}

// Push enqueues a task
func (q *queueImpl) Push(item Task) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()
	if !q.closing {
		q.tasks = append(q.tasks, item)
	}
	q.cond.Signal()
}

// Run start the task handler, which should be in a separate go routine.
// The queue can be closed via stop channel. However when it is being closed,
// the left elements in the queue will be processed.
// Note: The queue can be rerun only after closed.
func (q *queueImpl) Run(stop <-chan struct{}) {
	q.mutex.Lock()
	if !q.closed {
		q.mutex.Unlock()
		panic("queue can not be run twice")
	}
	q.closed = false
	q.mutex.Unlock()

	defer func() {
		q.mutex.Lock()
		// set closed status
		q.closed = true
		q.mutex.Unlock()
	}()

	// enable rerun the queue
	q.cond.L.Lock()
	q.closing = false
	q.cond.L.Unlock()
	go func() {
		<-stop
		q.cond.L.Lock()
		q.cond.Signal()
		q.closing = true
		q.cond.L.Unlock()
	}()

	for {
		q.cond.L.Lock()
		for !q.closing && len(q.tasks) == 0 {
			q.cond.Wait()
		}

		if len(q.tasks) == 0 {
			q.cond.L.Unlock()
			// We must be shutting down.
			return
		}

		var task Task
		task, q.tasks = q.tasks[0], q.tasks[1:]
		q.cond.L.Unlock()

		if err := task(); err != nil {
			log.Infof("Work item handle failed (%v), retry after delay %v", err, q.delay)
			time.AfterFunc(q.delay, func() {
				q.Push(task)
			})
		}
	}
}
