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
	"istio.io/istio/mixer/pkg/log"
	"istio.io/istio/mixer/pkg/pool"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/istio/mixer/pkg/template"
)

const InitialResultQueueSize = 16

// VectoredExecutor is used to perform scatter-gather calls against multiple handlers using go-routines, in a
// panic-safe manner.
// Aggregates results before returning.
type VectoredExecutor struct {

	// current result channel depth
	channelDepth int

	// channel for collecting results
	results     chan *result

	// The current number of outstanding operations
	vectorSize  int

	gp *pool.GoroutinePool
}

// Invoke the given call within a panic-protected context.
func (v *VectoredExecutor) Invoke(ctx context.Context, bag attribute.Bag, processor template.Processor, op string) {
	v.gp.ScheduleWork(func(){
		var res *result

		defer func() {
			if r := recover(); r != nil {
				// TODO: Obtain "op" from processor. It has the most context.
				log.Errorf("Invoke %s panic: %v", op, r)
				res = &result{
					err: fmt.Errorf("dispatch %s panic: %v", op, r),
				}
			}
		}()

		result, err := processor.Process(ctx, bag)
		if err != nil {
			res = &result{err: err}
		} else {
			res = &result{res:result}
		}

		v.results <- res
	})

	v.vectorSize++
}

func (v *VectoredExecutor) AggregateResults() (adapter.Result, error) {
	var res adapter.Result
	var err *multierror.Error
	var buf *bytes.Buffer
	code :=  google_rpc.OK

	for i := 0; i < v.vectorSize; i++ {
		rs := <- v.results

		if rs.err != nil {
			err = multierror.Append(err, rs.err)
		}
		if rs.res == nil { // When there is no return value like ProcessReport().
			continue
		}
		if res == nil {
			res = rs.res
		} else {
			if rc, ok := res.(*adapter.CheckResult); ok { // only check is expected to supported combining.
				rc.Combine(rs.res)
			}
		}
		st := rs.res.GetStatus()
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

// Create a new pool of vectored executorsg.
func NewVectoredExecutorPool(gp *pool.GoroutinePool) *VectoredExecutorPool {
	return &VectoredExecutorPool{
		pool: sync.Pool{
			New: func() interface{} {
				return &VectoredExecutor{
					results: make(chan *result, InitialResultQueueSize),
					gp: gp,
				}
			}},
	}
}

// VectoredExecutorPool is a pool of VectoredDispatchers.
type VectoredExecutorPool struct {
	pool sync.Pool
	}

func (p *VectoredExecutorPool) get(maxVectorSize int) *VectoredExecutor {
	dispatcher := p.pool.Get().(*VectoredExecutor)
	dispatcher.vectorSize = 0

	// Resize the channel to accommodate the vector, if necessary.
	if dispatcher.channelDepth < maxVectorSize {
		dispatcher.results = make(chan *result, maxVectorSize) // TODO: Use a larger allocation to avoid frequent re-allocs.
		dispatcher.channelDepth = maxVectorSize
	}

	return dispatcher
}

func (p *VectoredExecutorPool) put(d *VectoredExecutor) {
	d.gp = nil
	p.pool.Put(d)
}

// result encapsulates commonalities between all adapter returns.
type result struct {

	// all results return an error
	err error

	// CheckResult or QuotaResultLegacy
	res adapter.Result

	// TODO: replace this with a more appropriate result
	//// callinfo that resulted in "res". Used for informational purposes.
	//callinfo *Action
}

