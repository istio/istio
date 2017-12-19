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

package dispatcher

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/googleapis/googleapis/google/rpc"
	"github.com/hashicorp/go-multierror"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/pool"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/istio/pkg/log"
)

const queueAllocSize = 64

// execPool is used to perform scatter-gather calls against multiple handlers using go-routines, in a
// panic-safe manner.
type executor struct {

	// the maximum number of routines that can be executed side-by-side.
	maxParallelism int

	// channel for collecting results
	results chan result

	// The current number of outstanding operations
	outstanding int

	// The go-routine pool to schedule work on.
	gp *pool.GoroutinePool
}

func (v *executor) executeQuota(
	ctx context.Context,
	processFn template.ProcessQuota2Fn,
	handler adapter.Handler,
	args adapter.QuotaArgs,
	instance interface{}) {

	if v.outstanding == v.maxParallelism {
		panic("Request to execute more parallel tasks than can be handled.")
	}

	v.outstanding++

	v.gp.ScheduleWork(func() {
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("execute panic: %v", r)
				err := fmt.Errorf("dispatch panic: %v", r)
				v.results <- result{err: err}
			}
		}()

		res, err := processFn(ctx, handler, instance, args)
		v.results <- result{res: res, err: err}
	})
}

func (v *executor) executeReport(
	ctx context.Context,
	processFn template.ProcessReport2Fn,
	handler adapter.Handler,
	instances []interface{}) {

	if v.outstanding == v.maxParallelism {
		panic("Request to execute more parallel tasks than can be handled.")
	}

	v.outstanding++

	var err error
	v.gp.ScheduleWork(func() {
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("execute panic: %v", r)
				err = fmt.Errorf("dispatch panic: %v", r)
				v.results <- result{err: err}
			}
		}()

		err = processFn(ctx, handler, instances)
		v.results <- result{err: err}
	})
}

func (v *executor) executeCheck(
	ctx context.Context,
	processFn template.ProcessCheck2Fn,
	handler adapter.Handler,
	instance interface{}) {

	if v.outstanding == v.maxParallelism {
		panic("Request to execute more parallel tasks than can be handled.")
	}

	v.outstanding++

	v.gp.ScheduleWork(func() {
		defer func() {
			if r := recover(); r != nil {
				// TODO: Obtain "op" from Processor. It has the most context.
				log.Errorf("execute panic: %v", r)
				err := fmt.Errorf("dispatch panic: %v", r)
				v.results <- result{err: err}
			}
		}()

		res, err := processFn(ctx, handler, instance)
		v.results <- result{res: res, err: err}
	})
}

func (v *executor) executePreprocess(
	ctx context.Context,
	processFn template.ProcessGenAttrs2Fn,
	handler adapter.Handler,
	instance interface{},
	attrs attribute.Bag,
	mapper template.OutputMapperFn) {

	if v.outstanding == v.maxParallelism {
		panic("Request to execute more parallel tasks than can be handled.")
	}

	v.outstanding++

	v.gp.ScheduleWork(func() {
		defer func() {
			if r := recover(); r != nil {
				// TODO: Obtain "op" from Processor. It has the most context.
				log.Errorf("execute panic: %v", r)
				err := fmt.Errorf("dispatch panic: %v", r)
				v.results <- result{err: err}
			}
		}()

		res, err := processFn(ctx, handler, instance, attrs, mapper)
		v.results <- result{res: res, err: err}
	})
}

// wait for the completion of outstanding work and collect results.
func (v *executor) wait() (interface{}, error) {
	var res adapter.Result
	var err *multierror.Error
	var buf *bytes.Buffer
	code := google_rpc.OK

	for i := 0; i < v.outstanding; i++ {
		rs := <-v.results

		if rs.err != nil {
			err = multierror.Append(err, rs.err)
		}
		if rs.res == nil { // When there is no return value like ProcessReport().
			continue
		}
		if res == nil {
			res = rs.res.(adapter.Result)
		} else {
			if rc, ok := res.(*adapter.CheckResult); ok { // only check is expected to supported combining.
				rc.Combine(rs.res)
			}
		}
		st := rs.res.(adapter.Result).GetStatus()
		if !status.IsOK(st) {
			if buf == nil {
				buf = pool.GetBuffer()
				// the first failure result's code becomes the result code for the output
				code = google_rpc.Code(st.Code)
			} else {
				buf.WriteString(", ")
			}

			// TODO buf.WriteString(rs.callinfo.handlerName + ":" + st.Message)
		}
	}

	if buf != nil {
		res.SetStatus(status.WithMessage(code, buf.String()))
		pool.PutBuffer(buf)
	}

	return res, err.ErrorOrNil()
}

// Create a new pool of executors.
func newExecutorPool(gp *pool.GoroutinePool) *executorPool {
	return &executorPool{
		pool: sync.Pool{
			New: func() interface{} {
				return &executor{
					results: make(chan result, queueAllocSize),
					gp:      gp,
				}
			}},
	}
}

// executorPool is a pool of executors.
// Saves on allocation of result channels.
type executorPool struct {
	pool sync.Pool
}

// returns an execPool from the pool, with capability to execute the specified number of items.
func (p *executorPool) get(maxParallelism int) *executor {
	dispatcher := p.pool.Get().(*executor)
	dispatcher.outstanding = 0

	// Resize the channel to accommodate the parallelism, if necessary.
	if dispatcher.maxParallelism < maxParallelism {
		allocSize := ((maxParallelism / queueAllocSize) + 1) * queueAllocSize
		dispatcher.results = make(chan result, allocSize)
		dispatcher.maxParallelism = maxParallelism
	}

	return dispatcher
}

// returns the execPool back to the pool.
func (p *executorPool) put(d *executor) {
	d.gp = nil
	p.pool.Put(d)
}

// result encapsulates commonalities between all adapter returns.
type result struct {

	// all results return an error
	err error

	// CheckResult or QuotaResultLegacy
	res interface{}

	// TODO: replace this with a more appropriate result
	//// callinfo that resulted in "res". Used for informational purposes.
	//callinfo *Action
}
