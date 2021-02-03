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

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Task to be performed.
type Task func(entry cacheEntry)

// Worker queue implements an expandable goroutine pool which executes at most one concurrent routine per target
// resource.  Multiple calls to Push() will not schedule multiple executions per target resource, but will ensure that
// the single execution uses the latest value.
type WorkerQueue interface {
	// Push a task.
	Push(target Resource, progress Progress)
	// Run the loop until a signal on the context
	Run(ctx context.Context)
	// Delete a task
	Delete(target Resource)
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

type WorkQueue struct {
	// tasks which are not currently executing but need to run
	tasks []lockResource
	// a lock to govern access to data in the cache
	lock sync.Mutex
	// for each task, a cacheEntry which can be updated before the task is run so that execution will have latest values
	cache map[lockResource]cacheEntry

	OnPush func()
}

func (wq *WorkQueue) Push(target Resource, progress Progress) {
	wq.lock.Lock()
	key := convert(target)
	_, inqueue := wq.cache[key]
	wq.cache[key] = cacheEntry{
		cacheVal:      &target,
		cacheProgress: &progress,
	}
	if !inqueue {
		wq.tasks = append(wq.tasks, key)
	}
	wq.lock.Unlock()
	if wq.OnPush != nil {
		wq.OnPush()
	}
}

// Pop returns the first item in the queue not in exclusion, along with it's latest progress
func (wq *WorkQueue) Pop(exclusion map[lockResource]struct{}) (target *Resource, progress *Progress) {
	wq.lock.Lock()
	defer wq.lock.Unlock()
	for i := 0; i < len(wq.tasks); i++ {
		if _, ok := exclusion[wq.tasks[i]]; !ok {
			// remove from tasks
			t, ok := wq.cache[wq.tasks[i]]
			wq.tasks = append(wq.tasks[:i], wq.tasks[i+1:]...)
			if !ok {
				return nil, nil
			}
			return t.cacheVal, t.cacheProgress
		}
	}
	return nil, nil
}

func (wq *WorkQueue) Length() int {
	wq.lock.Lock()
	defer wq.lock.Unlock()
	return len(wq.tasks)
}

func (wq *WorkQueue) Delete(target *Resource) {
	wq.lock.Lock()
	defer wq.lock.Unlock()
	delete(wq.cache, convert(*target))
}

type WorkerPool struct {
	q WorkQueue
	// indicates the queue is closing
	closing bool
	// the function which will be run for each task in queue
	work func(*Resource, *Progress)
	// current worker routine count
	workerCount uint
	// maximum worker routine count
	maxWorkers       uint
	currentlyWorking map[lockResource]struct{}
	lock             sync.Mutex
}

func NewWorkerPool(work func(*Resource, *Progress), maxWorkers uint) WorkerQueue {
	return &WorkerPool{
		work:             work,
		maxWorkers:       maxWorkers,
		currentlyWorking: make(map[lockResource]struct{}),
		q: WorkQueue{
			tasks:  make([]lockResource, 0),
			cache:  make(map[lockResource]cacheEntry),
			OnPush: nil,
		},
	}
}

func (wp *WorkerPool) Delete(target Resource) {
	wp.q.Delete(&target)
}

func (wp *WorkerPool) Push(target Resource, progress Progress) {
	wp.q.Push(target, progress)
	wp.maybeAddWorker()
}

func (wp *WorkerPool) Run(ctx context.Context) {
	go func() {
		<-ctx.Done()
		wp.lock.Lock()
		wp.closing = true
		wp.lock.Unlock()
	}()
}

// maybeAddWorker adds a worker unless we are at maxWorkers.  Workers exit when there are no more tasks, except for the
// last worker, which stays alive indefinitely.
func (wp *WorkerPool) maybeAddWorker() {
	wp.lock.Lock()
	if wp.workerCount >= wp.maxWorkers || wp.q.Length() == 0 {
		wp.lock.Unlock()
		return
	}
	wp.workerCount++
	wp.lock.Unlock()
	go func() {
		for {
			wp.lock.Lock()
			if wp.closing || wp.q.Length() == 0 {
				wp.workerCount--
				wp.lock.Unlock()
				return
			}

			target, c := wp.q.Pop(wp.currentlyWorking)

			if target == nil {
				// continue or return?
				// could have been deleted, or could be no items in queueu not currently worked on.  need a way to differentiate.
				wp.lock.Unlock()
				continue
			}
			wp.q.Delete(target)
			wp.currentlyWorking[convert(*target)] = struct{}{}
			wp.lock.Unlock()
			// work should be done without holding the lock
			wp.work(target, c)

			wp.lock.Lock()
			delete(wp.currentlyWorking, convert(*target))
			wp.lock.Unlock()
		}
	}()
}
