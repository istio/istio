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
	"strconv"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"

	v1alpha12 "istio.io/api/analysis/v1alpha1"
	"istio.io/api/meta/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/util/sets"
)

// Task to be performed.
type Task func(entry cacheEntry)

// WorkerQueue implements an expandable goroutine pool which executes at most one concurrent routine per target
// resource.  Multiple calls to Push() will not schedule multiple executions per target resource, but will ensure that
// the single execution uses the latest value.
type WorkerQueue interface {
	// Push a task.
	Push(target Resource, controller *Controller, context any)
	// Run the loop until a signal on the context
	Run(ctx context.Context)
	// Delete a task
	Delete(target Resource)
}

type cacheEntry struct {
	// the cacheVale represents the latest version of the resource, including ResourceVersion
	cacheResource Resource
	// the perControllerStatus represents the latest version of the ResourceStatus
	perControllerStatus map[*Controller]any
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

func (wq *WorkQueue) Push(target Resource, ctl *Controller, progress any) {
	wq.lock.Lock()
	key := convert(target)
	if item, inqueue := wq.cache[key]; inqueue {
		item.perControllerStatus[ctl] = progress
		wq.cache[key] = item
	} else {
		wq.cache[key] = cacheEntry{
			cacheResource:       target,
			perControllerStatus: map[*Controller]any{ctl: progress},
		}
		wq.tasks = append(wq.tasks, key)
	}
	wq.lock.Unlock()
	if wq.OnPush != nil {
		wq.OnPush()
	}
}

// Pop returns the first item in the queue not in exclusion, along with it's latest progress
func (wq *WorkQueue) Pop(exclusion sets.Set[lockResource]) (target Resource, progress map[*Controller]any) {
	wq.lock.Lock()
	defer wq.lock.Unlock()
	for i := 0; i < len(wq.tasks); i++ {
		if !exclusion.Contains(wq.tasks[i]) {
			// remove from tasks
			t, ok := wq.cache[wq.tasks[i]]
			wq.tasks = append(wq.tasks[:i], wq.tasks[i+1:]...)
			if !ok {
				return Resource{}, nil
			}
			return t.cacheResource, t.perControllerStatus
		}
	}
	return Resource{}, nil
}

func (wq *WorkQueue) Length() int {
	wq.lock.Lock()
	defer wq.lock.Unlock()
	return len(wq.tasks)
}

func (wq *WorkQueue) Delete(target Resource) {
	wq.lock.Lock()
	defer wq.lock.Unlock()
	delete(wq.cache, convert(target))
}

type WorkerPool struct {
	q WorkQueue
	// indicates the queue is closing
	closing bool
	// the function which will be run for each task in queue
	write func(*config.Config)
	// the function to retrieve the initial status
	get func(Resource) *config.Config
	// current worker routine count
	workerCount uint
	// maximum worker routine count
	maxWorkers       uint
	currentlyWorking sets.Set[lockResource]
	lock             sync.Mutex
}

func NewWorkerPool(write func(*config.Config), get func(Resource) *config.Config, maxWorkers uint) WorkerQueue {
	return &WorkerPool{
		write:            write,
		get:              get,
		maxWorkers:       maxWorkers,
		currentlyWorking: sets.New[lockResource](),
		q: WorkQueue{
			tasks:  make([]lockResource, 0),
			cache:  make(map[lockResource]cacheEntry),
			OnPush: nil,
		},
	}
}

func (wp *WorkerPool) Delete(target Resource) {
	wp.q.Delete(target)
}

func (wp *WorkerPool) Push(target Resource, controller *Controller, context any) {
	wp.q.Push(target, controller, context)
	wp.maybeAddWorker()
}

func (wp *WorkerPool) Run(ctx context.Context) {
	context.AfterFunc(ctx, func() {
		wp.lock.Lock()
		wp.closing = true
		wp.lock.Unlock()
	})
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

			target, perControllerWork := wp.q.Pop(wp.currentlyWorking)

			if target == (Resource{}) {
				// could have been deleted, or could be no items in queue not currently worked on
				wp.lock.Unlock()
				return
			}
			wp.q.Delete(target)
			wp.currentlyWorking.Insert(convert(target))
			wp.lock.Unlock()
			// work should be done without holding the lock
			cfg := wp.get(target)
			if cfg != nil {
				// Check that generation matches
				if strconv.FormatInt(cfg.Generation, 10) == target.Generation {
					sm := GetStatusManipulator(cfg.Status)
					sm.SetObservedGeneration(cfg.Generation)
					for c, i := range perControllerWork {
						// TODO: this does not guarantee controller order.  perhaps it should?
						c.fn(sm, i)
					}
					cfg.Status = sm.Unwrap()
					wp.write(cfg)
				}
			}
			wp.lock.Lock()
			wp.currentlyWorking.Delete(convert(target))
			wp.lock.Unlock()
		}
	}()
}

// Manipulator gives controllers an opportunity to manipulate the status of an object.
// This allows the controller to be generic over the types of status messages it needs to handle.
type Manipulator interface {
	SetObservedGeneration(int64)
	SetValidationMessages(msgs diag.Messages)
	SetInner(c any)
	Unwrap() any
}

var (
	_ Manipulator = &IstioGenerationProvider{}
	_ Manipulator = &ServiceEntryGenerationProvider{}
	_ Manipulator = &NopStatusManipulator{}
)

type NopStatusManipulator struct {
	inner any
}

func (n *NopStatusManipulator) SetObservedGeneration(i int64) {
}

func (n *NopStatusManipulator) SetValidationMessages(msgs diag.Messages) {
}

func (n *NopStatusManipulator) Unwrap() any {
	return n.inner
}

func (n *NopStatusManipulator) SetInner(c any) {
	n.inner = c
}

type IstioGenerationProvider struct {
	*v1alpha1.IstioStatus
}

func (i *IstioGenerationProvider) SetInner(c any) {
	panic("not supported for this type")
}

func (i *IstioGenerationProvider) SetObservedGeneration(in int64) {
	i.ObservedGeneration = in
}

func (i *IstioGenerationProvider) Unwrap() any {
	return i.IstioStatus
}

func (i *IstioGenerationProvider) SetValidationMessages(msgs diag.Messages) {
	// zero out analysis messages, as this is the sole controller for those
	i.ValidationMessages = []*v1alpha12.AnalysisMessageBase{}
	for _, msg := range msgs {
		i.ValidationMessages = append(i.ValidationMessages, msg.AnalysisMessageBase())
	}
}

type ServiceEntryGenerationProvider struct {
	*networking.ServiceEntryStatus
}

func (i *ServiceEntryGenerationProvider) SetInner(c any) {
	panic("not supported for this type")
}

func (i *ServiceEntryGenerationProvider) SetObservedGeneration(in int64) {
	i.ObservedGeneration = in
}

func (i *ServiceEntryGenerationProvider) Unwrap() any {
	return i.ServiceEntryStatus
}

func (i *ServiceEntryGenerationProvider) SetValidationMessages(msgs diag.Messages) {
	// zero out analysis messages, as this is the sole controller for those
	i.ValidationMessages = []*v1alpha12.AnalysisMessageBase{}
	for _, msg := range msgs {
		i.ValidationMessages = append(i.ValidationMessages, msg.AnalysisMessageBase())
	}
}
