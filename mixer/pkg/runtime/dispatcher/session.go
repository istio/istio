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
	"strings"

	"github.com/gogo/googleapis/google/rpc"
	"github.com/hashicorp/go-multierror"
	"go.opencensus.io/stats"

	tpb "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/api/mixer/v1"
	descriptor "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/pool"
	"istio.io/istio/mixer/pkg/runtime/monitoring"
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
	quotaArgs    QuotaMethodArgs
	responseBag  *attribute.MutableBag
	reportStates map[*routing.Destination]*dispatchState

	// output parameters that get collected / accumulated as results.
	checkResult adapter.CheckResult
	quotaResult adapter.QuotaResult
	err         error

	// operation templates and directive
	operations []*routing.HeaderOperation
	directive  *v1.RouteDirective

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
	s.quotaArgs = QuotaMethodArgs{}
	s.responseBag = nil
	s.reportStates = nil

	s.activeDispatches = 0
	s.err = nil
	s.quotaResult = adapter.QuotaResult{}
	s.checkResult = adapter.CheckResult{}
	s.operations = nil
	s.directive = nil

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
	// Determine namespace to scope config resolution
	namespace, err := getIdentityNamespace(s.bag)
	if err != nil {
		// early return.
		stats.Record(s.ctx,
			monitoring.DestinationsPerRequest.M(0),
			monitoring.InstancesPerRequest.M(0))

		log.Warnf("unable to determine identity namespace: '%v', operation='%d'", err, s.variety)
		return err
	}
	destinations := s.rc.Routes.GetDestinations(s.variety, namespace)

	// Ensure that we can run dispatches to all destinations in parallel.
	s.ensureParallelism(len(destinations))

	foundQuota := false
	ninputs := 0
	ndestinations := 0
	for _, destination := range destinations {
		var state *dispatchState

		if s.variety == tpb.TEMPLATE_VARIETY_REPORT {
			// We buffer states for report calls and dispatch them later
			state = s.reportStates[destination]
			if state == nil {
				state = s.impl.getDispatchState(s.ctx, destination)
				s.reportStates[destination] = state
			}
		}

		for _, group := range destination.InstanceGroups {
			groupMatched := routing.Matches(group.Condition, s.bag)

			if groupMatched {
				ndestinations++
			}

			for j, input := range group.Builders {
				if s.variety == tpb.TEMPLATE_VARIETY_QUOTA {
					// only dispatch instances with a matching name
					if !strings.EqualFold(input.InstanceShortName, s.quotaArgs.Quota) {
						continue
					}
					if !groupMatched {
						// This is a conditional quota and it does not apply to the requester
						// return what was requested
						s.quotaResult.Amount = s.quotaArgs.Amount
						s.quotaResult.ValidDuration = defaultValidDuration
					}
					foundQuota = true
				}

				if !groupMatched {
					continue
				}

				var instance interface{}
				if instance, err = input.Builder(s.bag); err != nil {
					log.Errorf("error creating instance: destination='%v', error='%v'", destination.FriendlyName, err)
					s.err = multierror.Append(s.err, err)
					continue
				}
				ninputs++

				// For report templates, accumulate instances as much as possible before commencing dispatch.
				if s.variety == tpb.TEMPLATE_VARIETY_REPORT {
					state.instances = append(state.instances, instance)
					continue
				}

				// for other templates, dispatch for each instance individually.
				state = s.impl.getDispatchState(s.ctx, destination)
				state.actionName = input.ActionName
				state.evaluateFn = input.EvaluateFn
				state.instances = append(state.instances, instance)
				if s.variety == tpb.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR {
					state.mapper = group.Mappers[j]
					state.inputBag = s.bag
				}

				// Dispatch for singleton dispatches
				state.quotaArgs.BestEffort = s.quotaArgs.BestEffort
				state.quotaArgs.DeduplicationID = s.quotaArgs.DeduplicationID
				state.quotaArgs.QuotaAmount = s.quotaArgs.Amount
				s.dispatchToHandler(state)
			}
		}
	}

	if s.variety == tpb.TEMPLATE_VARIETY_CHECK {
		operationGroups := s.rc.Routes.GetOperations(namespace)
		for _, group := range operationGroups {
			if routing.Matches(group.Condition, s.bag) {
				s.operations = append(s.operations, group.Operations...)
			}
		}
	}

	stats.Record(s.ctx,
		monitoring.DestinationsPerRequest.M(int64(ndestinations)),
		monitoring.InstancesPerRequest.M(int64(ninputs)))

	s.waitForDispatched()

	if s.variety == tpb.TEMPLATE_VARIETY_QUOTA && !foundQuota {
		// If quota is not found it is very likely that quotaSpec / quotaSpecBinding was applied first
		// We still err on the side of allowing access, but warn about the fact that quota was not found.
		s.quotaResult.Amount = s.quotaArgs.Amount
		s.quotaResult.ValidDuration = defaultValidDuration
		log.Warnf("Requested quota '%s' is not configured", s.quotaArgs.Quota)
	}

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

type wrapperBag struct {
	attribute.Bag
	prefix   string
	evaluate func(string) (interface{}, bool)
}

func (b *wrapperBag) Get(name string) (interface{}, bool) {
	if strings.HasPrefix(name, b.prefix) {
		trimmed := strings.TrimPrefix(name, b.prefix)
		if val, ok := b.evaluate(trimmed); ok {
			return val, ok
		}
	}
	return b.Bag.Get(name)
}

var _ attribute.Bag = &wrapperBag{}

func (s *session) waitForDispatched() {
	// wait on the dispatch states and accumulate results
	var buf *bytes.Buffer
	code := rpc.OK

	// bag with action outputs
	bag := s.bag

	for s.activeDispatches > 0 {
		state := <-s.completed
		s.activeDispatches--

		// Aggregate errors
		if state.err != nil {
			s.err = multierror.Append(s.err, state.err)
		}

		st := rpc.Status{Code: int32(rpc.OK)}

		switch s.variety {
		case tpb.TEMPLATE_VARIETY_REPORT:
			// Do nothing

		case tpb.TEMPLATE_VARIETY_CHECK, tpb.TEMPLATE_VARIETY_CHECK_WITH_OUTPUT:
			if adapter.IsDefault(&s.checkResult) {
				// no results so far
				s.checkResult = state.checkResult
			} else {
				// combine with a previously obtained result
				if s.checkResult.ValidDuration > state.checkResult.ValidDuration {
					s.checkResult.ValidDuration = state.checkResult.ValidDuration
				}
				if s.checkResult.ValidUseCount > state.checkResult.ValidUseCount {
					s.checkResult.ValidUseCount = state.checkResult.ValidUseCount
				}
			}
			st = state.checkResult.Status
			if state.evaluateFn != nil {
				bag = &wrapperBag{
					Bag:      bag,
					prefix:   state.actionName + ".output.",
					evaluate: state.evaluateFn(state.checkOutput),
				}
			}

		case tpb.TEMPLATE_VARIETY_QUOTA:
			if s.quotaResult.IsDefault() {
				s.quotaResult = state.quotaResult
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

	if buf != nil {
		switch s.variety {
		case tpb.TEMPLATE_VARIETY_CHECK, tpb.TEMPLATE_VARIETY_CHECK_WITH_OUTPUT:
			s.checkResult.Status = status.WithMessage(code, buf.String())
		case tpb.TEMPLATE_VARIETY_QUOTA:
			s.quotaResult.Status = status.WithMessage(code, buf.String())
		}
		pool.PutBuffer(buf)
	}

	// compute route directive once all dispatches finish
	if s.variety == tpb.TEMPLATE_VARIETY_CHECK && status.IsOK(s.checkResult.Status) && len(s.operations) > 0 {
		s.directive = &v1.RouteDirective{}
		for _, op := range s.operations {
			name, nerr := op.Name.EvaluateString(bag)
			if nerr != nil {
				log.Warnf("Failed to evaluate header name: %v", nerr)
				continue
			}
			value, verr := op.Value.EvaluateString(bag)
			if verr != nil {
				log.Warnf("Failed to evaluate header value: %v", verr)
				continue
			}
			hop := v1.HeaderOperation{
				Name:  name,
				Value: value,
			}
			switch op.Operation {
			case descriptor.Rule_HeaderOperationTemplate_APPEND:
				hop.Operation = v1.APPEND
			case descriptor.Rule_HeaderOperationTemplate_REMOVE:
				hop.Operation = v1.REMOVE
			case descriptor.Rule_HeaderOperationTemplate_REPLACE:
				hop.Operation = v1.REPLACE
			}
			switch op.Type {
			case routing.RequestHeaderOperation:
				s.directive.RequestHeaderOperations = append(s.directive.RequestHeaderOperations, hop)
			case routing.ResponseHeaderOperation:
				s.directive.ResponseHeaderOperations = append(s.directive.ResponseHeaderOperations, hop)
			}
		}
	}
}
