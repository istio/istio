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

package status

import (
	"context"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/pkg/log"
)

// Task to be performed.
type Task func(entry *cacheEntry) error

// Worker queue implements an expandable goroutine pool which executes at most one concurrent routine per target
// resource.  Multiple calls to Push() will not schedule multiple executions per target resource, but will ensure that
// the single execution uses the latest value.
type WorkerQueue interface {
	// Push a task.
	Push(target Resource, progress Progress)
	// Run the loop until a signal on the context
	Run(ctx context.Context)
	// Delete a task
	Delete(target lockResource)
}

type queueImpl struct {
	delay   time.Duration
	tasks   []lockResource
	cond    *sync.Cond
	closing bool
	work    Task

	cacheLock   sync.Mutex
	cache       map[lockResource]*cacheEntry
	workerCount int
	maxWorkers  int
}

// NewQueue instantiates a queue with a processing function
func NewQueue(errorDelay time.Duration, work Task, maxWorkers int) WorkerQueue {
	return &queueImpl{
		delay:       errorDelay,
		tasks:       make([]lockResource, 0),
		closing:     false,
		cond:        sync.NewCond(&sync.Mutex{}),
		work:        work,
		workerCount: 0,
		maxWorkers:  maxWorkers,
		cache:       make(map[lockResource]*cacheEntry),
	}
}

func (q *queueImpl) Push(target Resource, progress Progress) {
	q.cacheLock.Lock()
	defer q.cacheLock.Unlock()
	key := convert(target)
	_, inqueue := q.cache[key]
	q.cache[key] = &cacheEntry{
		cacheVal:      &target,
		cacheProgress: &progress,
	}
	if !inqueue {
		q.enqueue(key)
	}
	q.maybeAddWorker()
}

func (q *queueImpl) Delete(target lockResource) {
	q.cacheLock.Lock()
	defer q.cacheLock.Unlock()
	delete(q.cache, target)
}

func (q *queueImpl) enqueue(item lockResource) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()
	if !q.closing {
		q.tasks = append(q.tasks, item)
	}
	q.cond.Signal()
}

func (q *queueImpl) maybeAddWorker() {
	q.cond.L.Lock()
	if q.workerCount >= q.maxWorkers {
		q.cond.L.Unlock()
		return
	}
	q.workerCount++
	q.cond.L.Unlock()
	go func() {
		for {
			q.cond.L.Lock()
			for !q.closing && len(q.tasks) == 0 {
				if q.workerCount > 1 {
					q.workerCount--
					q.cond.L.Unlock()
					return
				}
				q.cond.Wait()
			}

			if len(q.tasks) == 0 {
				q.cond.L.Unlock()
				// We must be shutting down.
				return
			}

			var target lockResource
			target, q.tasks = q.tasks[0], q.tasks[1:]
			q.cond.L.Unlock()

			var c *cacheEntry
			q.cacheLock.Lock()
			c, ok := q.cache[target]
			if !ok {
				// this element has been deleted, move along
				q.cacheLock.Unlock()
				continue
			} else {
				delete(q.cache, target)
			}
			q.cacheLock.Unlock()

			if err := q.work(c); err != nil {
				log.Infof("Work item handle failed (%v), retry after delay %v", err, q.delay)
				time.AfterFunc(q.delay, func() {
					q.Push(*c.cacheVal, *c.cacheProgress)
				})
			}
		}
	}()
}

func (q *queueImpl) Run(ctx context.Context) {
	go func() {
		<-ctx.Done()
		q.cond.L.Lock()
		q.cond.Signal()
		q.closing = true
		q.cond.L.Unlock()
	}()
	q.maybeAddWorker()
}

type cacheEntry struct {
	// the cacheVale represents the latest version of the resource, including ResourceVersion
	cacheVal *Resource
	// the cacheProgress represents the latest version of the Progress
	cacheProgress *Progress
}

type lockResource struct {
	schema.GroupVersionResource
	Namespace string
	Name      string
}

func convert(i Resource) lockResource {
	return lockResource{
		GroupVersionResource: i.GroupVersionResource,
		Namespace:            i.Namespace,
		Name:                 i.Name,
	}
}
