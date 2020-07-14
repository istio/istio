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

// Package dispatcher is used to dispatch incoming requests to one or more handlers. The main entry point
// is the Impl struct, which implements the main dispatcher.Dispatcher interface.
//
// Once the dispatcher receives a request, it acquires the current routing table, and uses it until the end
// of the request. Additionally, it acquires an executor from a pool, which is used to perform and track the
// parallel calls that will be performed against the handlers. The executors scatter the calls.
package dispatcher

import (
	"context"
	"sync"
	"time"

	tpb "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/runtime/routing"
	"istio.io/pkg/attribute"
	"istio.io/pkg/pool"
)

// Dispatcher dispatches incoming API calls to configured adapters.
type Dispatcher interface {
	// Preprocess dispatches to the set of adapters that will run before any
	// other adapters in Mixer (aka: the Check, Report, Quota adapters).
	Preprocess(ctx context.Context, requestBag attribute.Bag, responseBag *attribute.MutableBag) error

	// Check dispatches to the set of adapters associated with the Check API method
	Check(ctx context.Context, requestBag attribute.Bag) (adapter.CheckResult, error)

	// GetReporter get an interface where reports are buffered.
	GetReporter(ctx context.Context) Reporter

	// Quota dispatches to the set of adapters associated with the Quota API method
	Quota(ctx context.Context, requestBag attribute.Bag,
		qma QuotaMethodArgs) (adapter.QuotaResult, error)
}

// QuotaMethodArgs is supplied by invocations of the Quota method.
type QuotaMethodArgs struct {
	// Used for deduplicating quota allocation/free calls in the case of
	// failed RPCs and retries. This should be a UUID per call, where the same
	// UUID is used for retries of the same quota allocation call.
	DeduplicationID string

	// The quota to allocate from.
	Quota string

	// The amount of quota to allocate.
	Amount int64

	// If true, allows a response to return less quota than requested. When
	// false, the exact requested amount is returned or 0 if not enough quota
	// was available.
	BestEffort bool
}

// Impl is the runtime implementation of the Dispatcher interface.
type Impl struct {
	// Current routing context.
	rc *RoutingContext

	// the reader-writer lock for accessing or changing the context.
	rcLock sync.RWMutex

	// pool of sessions
	sessionPool sync.Pool

	// pool of dispatch states
	statePool sync.Pool

	// pool of reporters
	reporterPool sync.Pool

	// pool of goroutines
	gp *pool.GoroutinePool

	enableTracing bool
}

var _ Dispatcher = &Impl{}

// New returns a new Impl instance. The Impl instance is initialized with an empty routing table.
func New(handlerGP *pool.GoroutinePool, enableTracing bool) *Impl {
	d := &Impl{
		gp:            handlerGP,
		enableTracing: enableTracing,
		rc: &RoutingContext{
			Routes: routing.Empty(),
		},
	}

	d.sessionPool.New = func() interface{} { return &session{} }
	d.statePool.New = func() interface{} { return &dispatchState{} }
	d.reporterPool.New = func() interface{} { return &reporter{states: make(map[*routing.Destination]*dispatchState)} }

	return d
}

const (
	defaultValidDuration = 1 * time.Minute
	defaultValidUseCount = 10000
)

// Check implementation of runtime.Impl.
func (d *Impl) Check(ctx context.Context, bag attribute.Bag) (adapter.CheckResult, error) {
	s := d.getSession(ctx, tpb.TEMPLATE_VARIETY_CHECK, bag)
	// allocate bag for storing check output on top of input attributes
	s.responseBag = attribute.GetMutableBag(bag)

	var r adapter.CheckResult
	err := s.dispatch()
	if err == nil {
		r = s.checkResult
		err = s.err

		if err == nil {
			// No adapters chimed in on this request, so we return a "good to go" value which can be cached
			// for up to a minute.
			//
			// TODO: make these fallback values configurable
			if r.IsDefault() {
				r = adapter.CheckResult{
					ValidUseCount: defaultValidUseCount,
					ValidDuration: defaultValidDuration,
				}
			}
		}
	}

	s.responseBag.Done()
	d.putSession(s)
	return r, err
}

// GetReporter implementation of runtime.Impl.
func (d *Impl) GetReporter(ctx context.Context) Reporter {
	return d.getReporter(ctx)
}

// Quota implementation of runtime.Impl.
func (d *Impl) Quota(ctx context.Context, bag attribute.Bag, qma QuotaMethodArgs) (adapter.QuotaResult, error) {
	s := d.getSession(ctx, tpb.TEMPLATE_VARIETY_QUOTA, bag)
	s.quotaArgs = qma

	err := s.dispatch()
	if err == nil {
		err = s.err
	}
	qr := s.quotaResult

	d.putSession(s)
	return qr, err
}

// Preprocess implementation of runtime.Impl.
func (d *Impl) Preprocess(ctx context.Context, bag attribute.Bag, responseBag *attribute.MutableBag) error {
	s := d.getSession(ctx, tpb.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR, bag)
	s.responseBag = responseBag

	err := s.dispatch()
	if err == nil {
		err = s.err
	}

	d.putSession(s)
	return err
}

// Session template variety is CHECK for output producing templates (CHECK_WITH_OUTPUT)
func (d *Impl) getSession(context context.Context, variety tpb.TemplateVariety, bag attribute.Bag) *session {
	s := d.sessionPool.Get().(*session)

	s.impl = d
	s.rc = d.acquireRoutingContext()
	s.ctx = context
	s.variety = variety
	s.bag = bag

	return s
}

func (d *Impl) putSession(s *session) {
	s.rc.decRef()
	s.clear()
	d.sessionPool.Put(s)
}

func (d *Impl) getDispatchState(context context.Context, destination *routing.Destination) *dispatchState {
	ds := d.statePool.Get().(*dispatchState)

	ds.destination = destination
	ds.ctx = context

	return ds
}

func (d *Impl) putDispatchState(ds *dispatchState) {
	ds.clear()
	d.statePool.Put(ds)
}

func (d *Impl) getReporter(context context.Context) *reporter {
	r := d.reporterPool.Get().(*reporter)

	r.impl = d
	r.rc = d.acquireRoutingContext()
	r.ctx = context

	return r
}

func (d *Impl) putReporter(r *reporter) {
	r.rc.decRef()
	r.clear()
	d.reporterPool.Put(r)
}

func (d *Impl) acquireRoutingContext() *RoutingContext {
	d.rcLock.RLock()
	rc := d.rc
	rc.incRef()
	d.rcLock.RUnlock()

	return rc
}

// ChangeRoute changes the routing table on the Impl which, in turn, ends up creating a new RoutingContext.
func (d *Impl) ChangeRoute(newTable *routing.Table) *RoutingContext {
	newRC := &RoutingContext{
		Routes: newTable,
	}

	d.rcLock.Lock()
	old := d.rc
	d.rc = newRC
	d.rcLock.Unlock()

	return old
}
