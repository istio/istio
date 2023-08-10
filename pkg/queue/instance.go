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

	"go.uber.org/atomic"
	"k8s.io/apimachinery/pkg/util/rand"

	"istio.io/istio/pkg/log"
)

// Task to be performed.
type Task func() error

type queueTask struct {
	task        Task
	enqueueTime time.Time
	startTime   time.Time
}

// Instance of work tickets processed using a rate-limiting loop
type baseInstance interface {
	// Push a task.
	Push(task Task)
	// Run the loop until a signal on the channel
	Run(<-chan struct{})
	// Closed returns a chan that will be signaled when the Instance has stopped processing tasks.
	Closed() <-chan struct{}
}

type Instance interface {
	baseInstance
	// HasSynced returns true once the queue has synced.
	// Syncing indicates that all items in the queue *before* Run was called have been processed.
	HasSynced() bool
}

type queueImpl struct {
	delay     time.Duration
	tasks     []*queueTask
	cond      *sync.Cond
	closing   bool
	closed    chan struct{}
	closeOnce *sync.Once
	// initialSync indicates the queue has initially "synced".
	initialSync *atomic.Bool
	id          string
	metrics     *queueMetrics
}

// NewQueue instantiates a queue with a processing function
func NewQueue(errorDelay time.Duration) Instance {
	return NewQueueWithID(errorDelay, rand.String(10))
}

func NewQueueWithID(errorDelay time.Duration, name string) Instance {
	return &queueImpl{
		delay:       errorDelay,
		tasks:       make([]*queueTask, 0),
		closing:     false,
		closed:      make(chan struct{}),
		closeOnce:   &sync.Once{},
		initialSync: atomic.NewBool(false),
		cond:        sync.NewCond(&sync.Mutex{}),
		id:          name,
		metrics:     newQueueMetrics(name),
	}
}

func (q *queueImpl) Push(item Task) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()
	if !q.closing {
		q.tasks = append(q.tasks, &queueTask{task: item, enqueueTime: time.Now()})
		q.metrics.depth.RecordInt(int64(len(q.tasks)))
	}
	q.cond.Signal()
}

func (q *queueImpl) Closed() <-chan struct{} {
	return q.closed
}

// get blocks until it can return a task to be processed. If shutdown = true,
// the processing go routine should stop.
func (q *queueImpl) get() (task *queueTask, shutdown bool) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()
	// wait for closing to be set, or a task to be pushed
	for !q.closing && len(q.tasks) == 0 {
		q.cond.Wait()
	}

	if q.closing && len(q.tasks) == 0 {
		// We must be shutting down.
		return nil, true
	}
	task = q.tasks[0]
	// Slicing will not free the underlying elements of the array, so explicitly clear them out here
	q.tasks[0] = nil
	q.tasks = q.tasks[1:]

	task.startTime = time.Now()
	q.metrics.depth.RecordInt(int64(len(q.tasks)))
	q.metrics.latency.Record(time.Since(task.enqueueTime).Seconds())

	return task, false
}

func (q *queueImpl) processNextItem() bool {
	// Wait until there is a new item in the queue
	task, shuttingdown := q.get()
	if shuttingdown {
		return false
	}

	// Run the task.
	if err := task.task(); err != nil {
		delay := q.delay
		log.Infof("Work item handle failed (%v), retry after delay %v", err, delay)
		time.AfterFunc(delay, func() {
			q.Push(task.task)
		})
	}
	q.metrics.workDuration.Record(time.Since(task.startTime).Seconds())

	return true
}

func (q *queueImpl) HasSynced() bool {
	return q.initialSync.Load()
}

func (q *queueImpl) Run(stop <-chan struct{}) {
	log.Debugf("started queue %s", q.id)
	defer func() {
		q.closeOnce.Do(func() {
			log.Debugf("closed queue %s", q.id)
			close(q.closed)
		})
	}()
	go func() {
		<-stop
		q.cond.L.Lock()
		q.cond.Signal()
		q.closing = true
		q.cond.L.Unlock()
	}()

	q.Push(func() error {
		q.initialSync.Store(true)
		return nil
	})
	for q.processNextItem() {
	}
}
