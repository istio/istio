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
	"context"

	tpb "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/runtime/routing"
)

// Reporter is used to produce a series of reports
type Reporter interface {
	// Report adds an entry to the report state
	Report(requestBag attribute.Bag) error

	// Flush dispatches all buffered state to the appropriate adapters.
	Flush() error

	// Completes use of the reporter
	Done()
}

// reporter is the implementation of the Reporter interface
type reporter struct {
	impl   *Impl
	rc     *RoutingContext
	ctx    context.Context
	states map[*routing.Destination]*dispatchState
}

var _ Reporter = &reporter{}

func (r *reporter) clear() {
	r.impl = nil
	r.rc = nil
	r.ctx = nil

	for k := range r.states {
		delete(r.states, k)
	}
}

func (r *reporter) Report(bag attribute.Bag) error {
	s := r.impl.getSession(r.ctx, tpb.TEMPLATE_VARIETY_REPORT, bag)
	s.reportStates = r.states

	err := s.dispatch()
	if err == nil {
		err = s.err
	}

	r.impl.putSession(s)
	return err
}

func (r *reporter) Flush() error {
	s := r.impl.getSession(r.ctx, tpb.TEMPLATE_VARIETY_REPORT, nil)
	s.reportStates = r.states

	s.dispatchBufferedReports()
	err := s.err

	r.impl.putSession(s)
	return err
}

func (r *reporter) Done() {
	r.impl.putReporter(r)
}
