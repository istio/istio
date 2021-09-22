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

	"github.com/cenkalti/backoff"

	"istio.io/pkg/log"
)

// Queue of work tickets processed using a rate-limiting loop
type Queue interface {
	// Push a task.
	Push(item interface{}, task Task)
	// Run the loop until a signal on the channel
	Run(<-chan struct{})
}

type (
	empty struct{}
	t     interface{}
	set   map[t]empty
)

func (s set) has(item t) bool {
	_, exists := s[item]
	return exists
}

func (s set) insert(item t) {
	s[item] = empty{}
}

func (s set) delete(item t) {
	delete(s, item)
}

type innerTask struct {
	item interface{}
	*backoff.ExponentialBackOff
	task Task
}

type BackOffOption struct {
	// Initial backoff interval.
	InitialInterval time.Duration
	// Max Backoff interval.
	MaxInterval time.Duration
	// After MaxElapsedTime the ExponentialBackOff stops.
	// It never stops if MaxElapsedTime == 0.
	MaxElapsedTime time.Duration
}

type queue struct {
	backoffOpts BackOffOption
	tasks       []*innerTask
	// Things that are currently being processed are in the processing set.
	// These things may be simultaneously in the dirty set. When we finish
	// processing something and remove it from this set, we'll check if
	// it's in the dirty set, and if so, add it to the queue.
	processing set
	// dirty defines all of the items that need to be processed.
	// it has the latest task.
	dirty   map[interface{}]*innerTask
	cond    *sync.Cond
	closing bool
}

func NewBackOffQueue(backoff BackOffOption) Queue {
	return &queue{
		backoffOpts: backoff,
		tasks:       make([]*innerTask, 0),
		processing:  make(map[t]empty),
		dirty:       make(map[interface{}]*innerTask),
		closing:     false,
		cond:        sync.NewCond(&sync.Mutex{}),
	}
}

// NewFixedDelayQueue instantiates a queue with a processing function
func NewFixedDelayQueue(errorDelay time.Duration) Queue {
	return NewBackOffQueue(BackOffOption{
		InitialInterval: errorDelay,
		MaxInterval:     errorDelay,
		MaxElapsedTime:  0,
	})
}

func (q *queue) Push(item interface{}, task Task) {
	innerTask := &innerTask{item, newExponentialBackOff(q.backoffOpts), task}
	q.pushTask(innerTask)
}

func (q *queue) pushTask(task *innerTask) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()
	if q.closing {
		return
	}
	// If exist, update it; otherwise update it
	if q.dirty[task.item] != nil {
		q.dirty[task.item] = task
		return
	} else {
		q.dirty[task.item] = task
	}
	if q.processing.has(task.item) {
		return
	}

	q.tasks = append(q.tasks, task)
	q.cond.Signal()
}

func (q *queue) Run(stop <-chan struct{}) {
	go func() {
		<-stop
		q.cond.L.Lock()
		q.cond.Signal()
		q.closing = true
		q.cond.L.Unlock()
	}()

	for {
		innerTask, shutdown := q.get()
		if shutdown {
			log.Info("queue is shutting down")
			return
		}

		err := innerTask.task()
		q.done(innerTask.item)
		if err != nil {
			delay := innerTask.NextBackOff()
			if delay == backoff.Stop {
				log.Errorf("Drop work item (%v) because of exceeding MaxElapsedTime")
				break
			}
			log.Warnf("Work item (%v) handle failed (%v), retry after delay %v", innerTask.item, err, delay)
			time.AfterFunc(delay, func() {
				q.pushTask(innerTask)
			})
		}
	}
}

// get blocks until it can return an item to be processed. If shutdown = true,
// the caller should end their goroutine. You must call Done with item when you
// have finished processing it.
func (q *queue) get() (task *innerTask, shutdown bool) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()
	for len(q.tasks) == 0 && !q.closing {
		q.cond.Wait()
	}
	if len(q.tasks) == 0 {
		// We must be shutting down.
		return nil, true
	}

	task = q.tasks[0]
	// Slicing will not free the underlying elements of the array, so explicitly clear them out here
	q.tasks[0] = nil
	q.tasks = q.tasks[1:]

	// acquire the latest task
	if q.dirty[task.item] != nil {
		task = q.dirty[task.item]
		delete(q.dirty, task.item)
	}
	q.processing.insert(task)

	return task, false
}

// done marks item as done processing, and if it has been marked as dirty again
// while it was being processed, it will be re-added to the queue for
// re-processing.
func (q *queue) done(item interface{}) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	q.processing.delete(item)
	if task, ok := q.dirty[item]; ok {
		q.tasks = append(q.tasks, task)
		q.cond.Signal()
	}
}

func newExponentialBackOff(opts BackOffOption) *backoff.ExponentialBackOff {
	teb := backoff.NewExponentialBackOff()
	teb.InitialInterval = opts.InitialInterval
	teb.MaxElapsedTime = opts.MaxElapsedTime
	teb.MaxInterval = opts.MaxInterval
	return teb
}
