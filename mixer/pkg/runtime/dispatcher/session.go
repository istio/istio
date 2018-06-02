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
	"bytes"
	"context"

	"github.com/gogo/googleapis/google/rpc"
	multierror "github.com/hashicorp/go-multierror"

	tpb "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/pool"
	"istio.io/istio/mixer/pkg/runtime/config"
	"istio.io/istio/mixer/pkg/runtime/routing"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/istio/pkg/log"
)

const queueAllocSize = 64

// session represents a call session to the Impl. It contains all the mutable state needed for handling the
// call. It is used as temporary memory location to keep ephemeral state, thus avoiding garbage creation.
type session struct {
	// owner
	impl *Impl

	// routing context for the life of this session
	rc *RoutingContext

	// input parameters that was collected as part of the call.
	ctx          context.Context
	bag          attribute.Bag
	quotaArgs    adapter.QuotaArgs
	responseBag  *attribute.MutableBag
	reportStates map[*routing.Destination]*dispatchState

	// output parameters that gets collected / accumulated as result.
	checkResult *adapter.CheckResult
	quotaResult *adapter.QuotaResult
	err         error

	// The current number of activeDispatches handler dispatches.
	activeDispatches int

	// channel for collecting states of completed dispatches.
	completed chan *dispatchState

	// The variety of the operation that is being performed.
	variety tpb.TemplateVariety
}

func (s *session) clear() {
	s.impl = nil
	s.rc = nil
	s.variety = 0
	s.ctx = nil
	s.bag = nil
	s.quotaArgs = adapter.QuotaArgs{}
	s.responseBag = nil
	s.reportStates = nil

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

func (s *session) ensureParallelism(minParallelism int) {
	// Resize the channel to accommodate the parallelism, if necessary.
	if cap(s.completed) < minParallelism {
		allocSize := ((minParallelism / queueAllocSize) + 1) * queueAllocSize
		s.completed = make(chan *dispatchState, allocSize)
	}
}

func (s *session) dispatch() error {
	// Lookup the value of the identity attribute, so that we can extract the namespace to use for route
	// lookup.
	identityAttributeValue, err := getIdentityAttributeValue(s.bag, s.impl.identityAttribute)
	if err != nil {
		// early return.
		updateRequestCounters(0, 0)
		log.Warnf("unable to determine identity attribute value: '%v', operation='%d'", err, s.variety)
		return err
	}
	namespace := getNamespace(identityAttributeValue)
	destinations := s.rc.Routes.GetDestinations(s.variety, namespace)
	ctx := adapter.NewContextWithRequestData(s.ctx, &adapter.RequestData{adapter.Service{identityAttributeValue}})

	// TODO(Issue #2139): This is for old-style metadata based policy decisions. This should be eventually removed.
	ctxProtocol, _ := s.bag.Get(config.ContextProtocolAttributeName)
	tcp := ctxProtocol == config.ContextProtocolTCP

	// Ensure that we can run dispatches to all destinations in parallel.
	s.ensureParallelism(destinations.Count())

	ninputs := 0
	ndestinations := 0
	for _, destination := range destinations.Entries() {
		var state *dispatchState

		if s.variety == tpb.TEMPLATE_VARIETY_REPORT {
			// We buffer states for report calls and dispatch them later
			state = s.reportStates[destination]
			if state == nil {
				state = s.impl.getDispatchState(ctx, destination)
				s.reportStates[destination] = state
			}
		}

		for _, group := range destination.InstanceGroups {
			if !group.Matches(s.bag) || group.ResourceType.IsTCP() != tcp {
				continue
			}
			ndestinations++

			for j, input := range group.Builders {
				var instance interface{}
				if instance, err = input(s.bag); err != nil {
					log.Warnf("error creating instance: destination='%v', error='%v'", destination.FriendlyName, err)
					continue
				}
				ninputs++

				// For report templates, accumulate instances as much as possible before commencing dispatch.
				if s.variety == tpb.TEMPLATE_VARIETY_REPORT {
					state.instances = append(state.instances, instance)
					continue
				}

				// for other templates, dispatch for each instance individually.
				state = s.impl.getDispatchState(ctx, destination)
				state.instances = append(state.instances, instance)
				if s.variety == tpb.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR {
					state.mapper = group.Mappers[j]
					state.inputBag = s.bag
				}

				// Dispatch for singleton dispatches
				state.quotaArgs = s.quotaArgs
				s.dispatchToHandler(state)
			}
		}
	}

	updateRequestCounters(ndestinations, ninputs)
	s.waitForDispatched()

	return nil
}

func (s *session) dispatchBufferedReports() {
	// Ensure that we can run dispatches to all destinations in parallel.
	s.ensureParallelism(len(s.reportStates))

	// dispatch the buffered dispatchStates we've got
	for k, v := range s.reportStates {
		s.dispatchToHandler(v)
		delete(s.reportStates, k)
	}

	s.waitForDispatched()
}

func (s *session) dispatchToHandler(ds *dispatchState) {
	s.activeDispatches++
	ds.session = s
	s.impl.gp.ScheduleWork(ds.invokeHandler, nil)
}

func (s *session) waitForDispatched() {
	// wait on the dispatch states and accumulate results
	var buf *bytes.Buffer
	code := rpc.OK

	var err error
	for s.activeDispatches > 0 {
		state := <-s.completed
		s.activeDispatches--

		// Aggregate errors
		if state.err != nil {
			err = multierror.Append(err, state.err)
		}

		st := rpc.Status{Code: int32(rpc.OK)}

		switch s.variety {
		case tpb.TEMPLATE_VARIETY_REPORT:
			// Do nothing

		case tpb.TEMPLATE_VARIETY_CHECK:
			if s.checkResult == nil {
				r := state.checkResult
				s.checkResult = &r
			} else {
				if s.checkResult.ValidDuration > state.checkResult.ValidDuration {
					s.checkResult.ValidDuration = state.checkResult.ValidDuration
				}
				if s.checkResult.ValidUseCount > state.checkResult.ValidUseCount {
					s.checkResult.ValidUseCount = state.checkResult.ValidUseCount
				}
			}
			st = state.checkResult.Status

		case tpb.TEMPLATE_VARIETY_QUOTA:
			if s.quotaResult == nil {
				r := state.quotaResult
				s.quotaResult = &r
			} else {
				log.Warnf("Skipping quota op result due to previous value: '%v', op: '%s'",
					state.quotaResult, state.destination.FriendlyName)
			}
			st = state.quotaResult.Status

		case tpb.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR:
			if state.outputBag != nil {
				s.responseBag.Merge(state.outputBag)
			}
		}

		if !status.IsOK(st) {
			if buf == nil {
				buf = pool.GetBuffer()
				// the first failure result's code becomes the result code for the output
				code = rpc.Code(st.Code)
			} else {
				buf.WriteString(", ")
			}

			buf.WriteString(state.destination.HandlerName + ":" + st.Message)
		}

		s.impl.putDispatchState(state)
	}
	s.err = err

	if buf != nil {
		switch s.variety {
		case tpb.TEMPLATE_VARIETY_CHECK:
			s.checkResult.Status = status.WithMessage(code, buf.String())
		case tpb.TEMPLATE_VARIETY_QUOTA:
			s.quotaResult.Status = status.WithMessage(code, buf.String())
		}
		pool.PutBuffer(buf)
	}
}
