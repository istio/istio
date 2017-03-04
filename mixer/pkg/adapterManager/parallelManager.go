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

package adapterManager

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/pool"
	"istio.io/mixer/pkg/status"
)

// ParallelManager is an istio.io/mixer/pkg/api.Executor which wraps a Manager. Its Execute method calls the
// wrapped Manager's Execute in parallel across a pool of goroutines. The pool of worker goroutines is shared by all
// request goroutines, i.e. it's global.
type ParallelManager struct {
	*Manager

	work chan<- task     // Used to hand tasks to the workers in the pool
	quit chan<- struct{} // Used to shutdown the workers in the pool
	wg   *sync.WaitGroup // Used to block shutdown until all workers complete
}

// NewParallelManager returns a Manager who's Execute method calls the provided manager's Execute on each config in parallel.
// size is the number of worker go routines in the worker pool the Manager schedules on to. This pool of workers is global:
// the size of the pool should be roughly (max outstanding requests allowed)*(number of configs executed per request).
func NewParallelManager(manager *Manager, size int) *ParallelManager {
	work := make(chan task, size)
	quit := make(chan struct{})
	p := &ParallelManager{
		Manager: manager,
		work:    work,
		quit:    quit,
		wg:      &sync.WaitGroup{},
	}

	p.wg.Add(size)
	for i := 0; i < size; i++ {
		go p.worker(work, quit, p.wg)
	}

	return p
}

// Execute takes a set of configurations and uses the ParallelManager's embedded Manager to execute all of them in parallel.
func (p *ParallelManager) Execute(ctx context.Context, cfgs []*config.Combined, attrs attribute.Bag, ma aspect.APIMethodArgs) aspect.Output {
	numCfgs := len(cfgs)
	// TODO: look into pooling both result array and channel, they're created per-request and are constant size for cfg lifetime.
	results := make([]result, numCfgs)
	r := make(chan result, numCfgs)
	for _, cfg := range cfgs {
		// Take whichever case happens first: the context being canceled or a worker freeing up to accept our task.
		select {
		case <-ctx.Done():
			return aspect.Output{Status: status.WithDeadlineExceeded(fmt.Sprintf("failed to enqueue all config executions with err: %v", ctx.Err()))}
		case p.work <- task{ctx, cfg, attrs, ma, r}:
		}
	}

	for i := 0; i < numCfgs; i++ {
		select {
		case <-ctx.Done():
			return aspect.Output{Status: status.WithDeadlineExceeded(fmt.Sprintf("deadline exceeded waiting for adapter results with err: %v", ctx.Err()))}
		case res := <-r:
			results[i] = res
		}
	}

	return combineResults(results)
}

// Combines a bunch of distinct result structs and turns 'em into one single Output struct
func combineResults(results []result) aspect.Output {
	var buf *bytes.Buffer
	code := rpc.OK

	for _, r := range results {
		if !r.out.IsOK() {
			if buf == nil {
				buf = pool.GetBuffer()
				// the first failure result's code becomes the result code for the output
				code = rpc.Code(r.out.Status.Code)
			} else {
				buf.WriteString(", ")
			}
			buf.WriteString(r.cfg.String() + ":" + r.out.Message())
		}
	}

	s := status.OK
	if buf != nil {
		s = status.WithMessage(code, buf.String())
		pool.PutBuffer(buf)
	}

	return aspect.Output{Status: s}
}

// Shutdown gracefully drains the ParallelManager's worker pool and terminates the worker go routines.
func (p *ParallelManager) Shutdown() {
	close(p.quit)
	p.wg.Wait()
}

// result holds the values returned by the execution of an adapter
type result struct {
	cfg *config.Combined
	out aspect.Output
}

// task describes one unit of work to be executed by the pool
type task struct {
	ctx  context.Context
	cfg  *config.Combined
	ab   attribute.Bag
	ma   aspect.APIMethodArgs
	done chan<- result
}

// worker grabs a task off the queue, executes it, then blocks for the next signal (either a quit or another task).
func (p *ParallelManager) worker(task <-chan task, quit <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case t := <-task:
			out := p.execute(t.ctx, t.cfg, t.ab, t.ma)
			t.done <- result{t.cfg, out}
		case <-quit:
			return
		}
	}
}
