// Copyright 2018 Istio Authors
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
	"context"
	"sync"
	"time"

	tpb "istio.io/api/mixer/v1/template"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/runtime"
	"istio.io/istio/pkg/log"
)

const queueAllocSize = 64

// session represents a call session to the Dispatcher. It contains all the mutable state needed for handling the
// call. It is used as temporary memory location to keep ephemeral state, thus avoiding garbage creation.
type session struct {

	// The variety of the operation that is being performed.
	variety tpb.TemplateVariety

	// start time of the session.
	start time.Time

	// input parameters that was collected as part of the call.
	ctx             context.Context
	bag             attribute.Bag
	quotaMethodArgs runtime.QuotaMethodArgs
	responseBag     *attribute.MutableBag

	// output parameters that gets collected / accumulated as result.
	checkResult *adapter.CheckResult
	quotaResult *adapter.QuotaResult
	err         error

	//  handler dispatching related parameters

	// The current number of activeDispatches handler dispatches.
	activeDispatches int

	// channel for collecting states of completed dispatches.
	completed chan *dispatchState

	// whether to trace spans or not
	trace bool
}

// pool of sessions
type sessionPool struct {
	sessions sync.Pool
}

func (s *session) ensureParallelism(minParallelism int) {
	// Resize the channel to accommodate the parallelism, if necessary.
	if cap(s.completed) < minParallelism {
		allocSize := ((minParallelism / queueAllocSize) + 1) * queueAllocSize
		s.completed = make(chan *dispatchState, allocSize)
	}
}

func (s *session) clear() {
	s.variety = 0
	s.ctx = nil
	s.bag = nil
	s.quotaMethodArgs = runtime.QuotaMethodArgs{}
	s.responseBag = nil

	s.start = time.Time{}

	s.activeDispatches = 0
	s.err = nil
	s.quotaResult = nil
	s.checkResult = nil

	// Drain the channel
	exit := false
	for !exit {
		select {
		case <-s.completed:
			log.Warn("Leaked dispatch state discovered!")
			continue
		default:
			exit = true
		}
	}
}

// returns a new pool of sessions that uses the provided go-routine pool to execute dispatches.
func newSessionPool(enableTracing bool) *sessionPool {
	return &sessionPool{
		sessions: sync.Pool{
			New: func() interface{} {
				return &session{
					trace: enableTracing,
				}
			},
		},
	}
}

// returns a session from the pool that can support the specified number of parallel executions.
func (p *sessionPool) get() *session {
	session := p.sessions.Get().(*session)

	return session
}

func (p *sessionPool) put(session *session) {
	session.clear()
	p.sessions.Put(session)
}
