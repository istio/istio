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

	"github.com/cenkalti/backoff/v4"

	"istio.io/pkg/log"
)

// Task to be performed.
type Task func() error

type BackoffTask struct {
	task    Task
	backoff *backoff.ExponentialBackOff
}

// Instance of work tickets processed using a rate-limiting loop
type Instance interface {
	// Push a task.
	Push(task Task)
	// Run the loop until a signal on the channel
	Run(<-chan struct{})
}

type queueImpl struct {
	delay        time.Duration
	retryBackoff *backoff.ExponentialBackOff
	tasks        []*BackoffTask
	cond         *sync.Cond
	closing      bool
}

func newExponentialBackOff(eb *backoff.ExponentialBackOff) *backoff.ExponentialBackOff {
	if eb == nil {
		return nil
	}
	teb := backoff.NewExponentialBackOff()
	teb.InitialInterval = eb.InitialInterval
	teb.MaxElapsedTime = eb.MaxElapsedTime
	teb.MaxInterval = eb.MaxInterval
	teb.Multiplier = eb.Multiplier
	teb.RandomizationFactor = eb.RandomizationFactor
	return teb
}

// NewQueue instantiates a queue with a processing function
func NewQueue(errorDelay time.Duration) Instance {
	return &queueImpl{
		delay:   errorDelay,
		tasks:   make([]*BackoffTask, 0),
		closing: false,
		cond:    sync.NewCond(&sync.Mutex{}),
	}
}

func NewBackOffQueue(backoff *backoff.ExponentialBackOff) Instance {
	return &queueImpl{
		retryBackoff: backoff,
		tasks:        make([]*BackoffTask, 0),
		closing:      false,
		cond:         sync.NewCond(&sync.Mutex{}),
	}
}

func (q *queueImpl) Push(item Task) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()
	if !q.closing {
		q.tasks = append(q.tasks, &BackoffTask{item, newExponentialBackOff(q.retryBackoff)})
	}
	q.cond.Signal()
}

func (q *queueImpl) pushRetryTask(item *BackoffTask) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()
	if !q.closing {
		q.tasks = append(q.tasks, item)
	}
	q.cond.Signal()
}

func (q *queueImpl) Run(stop <-chan struct{}) {
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

		backoffTask := q.tasks[0]
		// Slicing will not free the underlying elements of the array, so explicitly clear them out here
		q.tasks[0] = nil
		q.tasks = q.tasks[1:]

		q.cond.L.Unlock()

		if err := backoffTask.task(); err != nil {
			delay := q.delay
			if q.retryBackoff != nil {
				delay = backoffTask.backoff.NextBackOff()
			}
			log.Infof("Work item handle failed (%v), retry after delay %v", err, delay)
			time.AfterFunc(delay, func() {
				q.pushRetryTask(backoffTask)
			})
		}
	}
}
