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

package pool

import (
	"sync"
)

// WorkFunc represents a function to invoke from a worker. The parameter is passed on the side
// to avoid creating closures and allocating.
type WorkFunc func(param interface{})

// GoroutinePool represents a set of reusable goroutines onto which work can be scheduled.
type GoroutinePool struct {
	queue          chan work      // Channel providing the work that needs to be executed
	wg             sync.WaitGroup // Used to block shutdown until all workers complete
	singleThreaded bool           // Whether to actually use goroutines or not
}

type work struct {
	fn    WorkFunc
	param interface{}
}

// NewGoroutinePool creates a new pool of goroutines to schedule async work.
func NewGoroutinePool(queueDepth int, singleThreaded bool) *GoroutinePool {
	return &GoroutinePool{
		queue:          make(chan work, queueDepth),
		singleThreaded: singleThreaded,
	}
}

// Close waits for all goroutines to terminate (and implements io.Closer).
func (gp *GoroutinePool) Close() error {
	if !gp.singleThreaded {
		close(gp.queue)
		gp.wg.Wait()
	}
	return nil
}

// ScheduleWork registers the given function to be executed at some point. The given param will
// be supplied to the function during execution.
//
// By making use of the supplied parameter, it is possible to avoid allocation costs when scheduling the
// work function. The caller needs to make sure that function fn only depends on external data through the
// passed in param interface{} and nothing else.
//
// A way to ensure this condition is met, is by passing ScheduleWork a normal named function rather than an
// inline anonymous function. It's easy for code in an anonymous function to have (or gain) an accidental
// reference that causes a closure to silently be created.
func (gp *GoroutinePool) ScheduleWork(fn WorkFunc, param interface{}) {
	if gp.singleThreaded {
		fn(param)
	} else {
		gp.queue <- work{fn: fn, param: param}
	}
}

// AddWorkers introduces more goroutines in the worker pool, increasing potential parallelism.
func (gp *GoroutinePool) AddWorkers(numWorkers int) {
	if !gp.singleThreaded {
		gp.wg.Add(numWorkers)
		for i := 0; i < numWorkers; i++ {
			go func() {
				for work := range gp.queue {
					work.fn(work.param)
				}

				gp.wg.Done()
			}()
		}
	}
}
