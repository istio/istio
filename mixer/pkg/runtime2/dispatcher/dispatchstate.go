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
	"sync"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/runtime2/routing"
	"istio.io/istio/mixer/pkg/template"
)

// dispatchState keeps the input/output state during the dispatch to a handler. It is used as temporary
// memory location to keep ephemeral state, thus avoiding garbage creation.
type dispatchState struct {
	session *session

	destination *routing.Destination
	mapper      template.OutputMapperFn

	inputBag  attribute.Bag
	quotaArgs adapter.QuotaArgs
	instance  interface{}
	instances []interface{}

	// output state that was collected from the handler.
	err         error
	outputBag   *attribute.MutableBag
	checkResult adapter.CheckResult
	quotaResult adapter.QuotaResult
}

func (s *dispatchState) clear() {
	s.session = nil
	s.destination = nil
	s.mapper = nil
	s.inputBag = nil
	s.quotaArgs = adapter.QuotaArgs{}
	s.instance = nil
	s.err = nil
	s.outputBag = nil
	s.checkResult = adapter.CheckResult{}
	s.quotaResult = adapter.QuotaResult{}

	if s.instances != nil {
		// re-slice to change the length to 0 without changing capacity.
		s.instances = s.instances[:0]
	}
}

// pool of dispatchStates
type dispatchStatePool struct {
	pool sync.Pool
}

func newDispatchStatePool() *dispatchStatePool {
	return &dispatchStatePool{
		pool: sync.Pool{
			New: func() interface{} {
				return &dispatchState{}
			},
		},
	}
}

func (p *dispatchStatePool) get(session *session, destination *routing.Destination) *dispatchState {
	s := p.pool.Get().(*dispatchState)
	s.session = session
	s.destination = destination
	return s
}

func (p *dispatchStatePool) put(s *dispatchState) {
	s.clear()
	p.pool.Put(s)
}
