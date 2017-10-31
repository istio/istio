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

// WorkFunc represents a function to invoke from a worker.
type WorkFunc func()

// GoroutinePool represents a set of reusable goroutines onto which work can be scheduled.
type GoroutinePool struct {
	queue          chan WorkFunc  // Channel providing the work that needs to be executed
	wg             sync.WaitGroup // Used to block shutdown until all workers complete
	singleThreaded bool           // Whether to actually use goroutines or not
}

// NewGoroutinePool creates a new pool of goroutines to schedule async work.
func NewGoroutinePool(queueDepth int, singleThreaded bool) *GoroutinePool {
	gp := &GoroutinePool{
		queue:          make(chan WorkFunc, queueDepth),
		singleThreaded: singleThreaded,
	}

	gp.AddWorkers(1)
	return gp
}

// Close waits for all goroutines to terminate
func (gp *GoroutinePool) Close() {
	if !gp.singleThreaded {
		close(gp.queue)
		gp.wg.Wait()
	}
}

// ScheduleWork registers the given function to be executed at some point
func (gp *GoroutinePool) ScheduleWork(fn WorkFunc) {
	if gp.singleThreaded {
		fn()
	} else {
		gp.queue <- fn
	}
}

// AddWorkers introduces more goroutines in the worker pool, increasing potential parallelism.
func (gp *GoroutinePool) AddWorkers(numWorkers int) {
	if !gp.singleThreaded {
		gp.wg.Add(numWorkers)
		for i := 0; i < numWorkers; i++ {
			go func() {
				for fn := range gp.queue {
					fn()
				}

				gp.wg.Done()
			}()
		}
	}
}
