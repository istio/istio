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

package dispatcher

import (
	"bytes"
	"context"
	"strings"

	multierror "github.com/hashicorp/go-multierror"
	"go.opencensus.io/stats"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	tpb "istio.io/api/mixer/adapter/model/v1beta1"
	mixerpb "istio.io/api/mixer/v1"
	descriptor "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/runtime/monitoring"
	"istio.io/istio/mixer/pkg/runtime/routing"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/pkg/log"
	"istio.io/pkg/pool"
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

func (s *session) dispatch() error { //nolint: unparam
	// Determine namespace to scope config resolution
	namespace := getIdentityNamespace(s.bag)
	destinations := s.rc.Routes.GetDestinations(s.variety, namespace)

	// Ensure that we can run dispatches to all destinations in parallel.
	s.ensureParallelism(destinations.Count())

	foundQuota := false
	ninputs := 0
	ndestinations := 0

	for _, destination := range destinations.Entries() {
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
			groupMatched := group.Matches(s.bag)

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
				var err error
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
				state.instances = append(state.instances, instance)
				if s.variety == tpb.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR {
					state.mapper = group.Mappers[j]
					state.inputBag = s.bag
				}

				// Dispatch for singleton dispatches
				state.quotaArgs.BestEffort = s.quotaArgs.BestEffort
				state.quotaArgs.DeduplicationID = s.quotaArgs.DeduplicationID
				state.quotaArgs.QuotaAmount = s.quotaArgs.Amount

				state.outputPrefix = input.ActionName + ".output."

				// TODO(kuat) make output bag concurrency safe
				s.dispatchToHandler(state)
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

	// aggregate header operations after filtering by attribute conditions
	if s.variety == tpb.TEMPLATE_VARIETY_CHECK && status.IsOK(s.checkResult.Status) {
		for _, directiveGroup := range destinations.Directives() {
			if directiveGroup.Condition != nil {
				if matches, err := directiveGroup.Condition.EvaluateBoolean(s.bag); err != nil || !matches {
					continue
				}
			}

			for _, op := range directiveGroup.Operations {
				hop := mixerpb.HeaderOperation{
					Name: op.HeaderName,
				}
				switch op.Operation {
				case descriptor.APPEND:
					hop.Operation = mixerpb.APPEND
				case descriptor.REMOVE:
					hop.Operation = mixerpb.REMOVE
				case descriptor.REPLACE:
					hop.Operation = mixerpb.REPLACE
				}

				if op.Operation != descriptor.REMOVE {
					var verr error
					hop.Value, verr = op.HeaderValue.EvaluateString(s.responseBag)
					if verr != nil {
						log.Warnf("Failed to evaluate header value: %v", verr)
						continue
					}
					if hop.Value == "" {
						continue
					}
				}

				// default response if RouteDirective is only action
				if s.checkResult.IsDefault() {
					s.checkResult = adapter.CheckResult{
						ValidUseCount: defaultValidUseCount,
						ValidDuration: defaultValidDuration,
					}
				}

				if s.checkResult.RouteDirective == nil {
					s.checkResult.RouteDirective = &mixerpb.RouteDirective{}
				}

				switch op.Type {
				case routing.RequestHeaderOperation:
					s.checkResult.RouteDirective.RequestHeaderOperations = append(s.checkResult.RouteDirective.RequestHeaderOperations, hop)
				case routing.ResponseHeaderOperation:
					s.checkResult.RouteDirective.ResponseHeaderOperations = append(s.checkResult.RouteDirective.ResponseHeaderOperations, hop)
				}
			}
		}
	}

	return nil
}

func (s *session) dispatchBufferedReports() {
	// Ensure that we can run dispatches to all destinations in parallel.
	s.ensureParallelism(len(s.reportStates))

	// dispatch the buffered dispatchStates we've got
	for k, v := range s.reportStates {
		if len(v.instances) == 0 {
			// do not dispatch to handler if nothing is buffered
			continue
		}
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

		case tpb.TEMPLATE_VARIETY_CHECK:
			if s.checkResult.IsDefault() {
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

			if state.outputBag != nil {
				s.responseBag.Merge(state.outputBag)
				state.outputBag.Done()
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
				// `buf` variable guards the first failure since it is set the first time
				code = rpc.Code(st.Code)

				// update the direct response matching the error status
				if s.variety == tpb.TEMPLATE_VARIETY_CHECK {
					if response := status.GetDirectHTTPResponse(st); response != nil {
						s.handleDirectResponse(st, response)
					}
				}
			} else {
				buf.WriteString(", ")
			}

			buf.WriteString(state.destination.HandlerName + ":" + st.Message)
		}

		s.impl.putDispatchState(state)
	}

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

func (s *session) handleDirectResponse(st rpc.Status, response *descriptor.DirectHttpResponse) {
	if s.checkResult.RouteDirective == nil {
		s.checkResult.RouteDirective = &mixerpb.RouteDirective{}
	}
	directive := s.checkResult.RouteDirective
	if response.Code != 0 {
		directive.DirectResponseCode = uint32(response.Code)
	} else {
		directive.DirectResponseCode = uint32(status.HTTPStatusFromCode(rpc.Code(st.Code)))
	}
	directive.DirectResponseBody = response.Body
	for header, value := range response.Headers {
		if !strings.EqualFold(header, "Set-Cookie") {
			directive.ResponseHeaderOperations = append(directive.ResponseHeaderOperations,
				mixerpb.HeaderOperation{
					Operation: mixerpb.REPLACE,
					Name:      header,
					Value:     value,
				})
		} else { // append Set-Cookie headers in multiple lines
			// Folded cookie syntax can be complicated. See, for example,
			// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Set-Cookie#Syntax.
			// Unfortunately, the net/http library does not expose/support cookie parsing.
			// Here we handle a much simpler syntax - a comma separated list of cookies.
			// The simplification comes at the cost of preventing the use of 'Expires'
			// cookie attribute. A more robust parser would require 100-150 LOC and could
			// be modeled after the following JS or Java code:
			// https://github.com/nfriedly/set-cookie-parser/blob/master/lib/set-cookie.js
			// or
			// https://github.com/google/j2objc/commit/16820fdbc8f76ca0c33472810ce0cb03d20efe25
			cookies := strings.Split(value, ",")
			for _, cv := range cookies {
				directive.ResponseHeaderOperations = append(directive.ResponseHeaderOperations,
					mixerpb.HeaderOperation{
						Operation: mixerpb.APPEND,
						Name:      header,
						Value:     cv,
					})
			}
		}
	}
}
