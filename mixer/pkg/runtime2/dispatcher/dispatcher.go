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

// Package dispatcher is used to dispatch incoming requests to one or more handlers. The main entry point
// is the Dispatcher struct, which implements the main runtime.Dispatcher interface.
//
// Once the dispatcher receives a request, it acquires the current routing table, and uses it until the end
// of the request. Additionally, it acquires an executor from a pool, which is used to perform and track the
// parallel calls that will be performed against the handlers. The executors scatter the calls
package dispatcher

import (
	"bytes"
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	multierror "github.com/hashicorp/go-multierror"
	opentracing "github.com/opentracing/opentracing-go"

	tpb "istio.io/api/mixer/v1/template"
	rpc "istio.io/gogo-genproto/googleapis/google/rpc"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/pool"
	"istio.io/istio/mixer/pkg/runtime"
	"istio.io/istio/mixer/pkg/runtime2/config"
	"istio.io/istio/mixer/pkg/runtime2/routing"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/istio/pkg/log"
)

// Dispatcher is the runtime2 implementation of the runtime.Dispatcher interface.
type Dispatcher struct {
	identityAttribute string

	// Current dispatch context.
	context *RoutingContext

	// the reader-writer lock for accessing or changing the context.
	contextLock sync.RWMutex

	// pool of sessions
	sessionPool *sessionPool

	// pool of dispatch states
	statePool *dispatchStatePool

	gp *pool.GoroutinePool
}

var _ runtime.Dispatcher = &Dispatcher{}

// RoutingContext is the currently active dispatching context, based on a config snapshot. As config changes,
// the current/live RoutingContext also changes.
type RoutingContext struct {
	// the routing table of this context.
	Routes *routing.Table

	// the current reference count. Indicates how many calls are currently using this RoutingContext.
	refCount int32
}

// IncRef increases the reference count on the RoutingContext.
func (t *RoutingContext) IncRef() {
	atomic.AddInt32(&t.refCount, 1)
}

// DecRef decreases the reference count on the RoutingContext.
func (t *RoutingContext) DecRef() {
	atomic.AddInt32(&t.refCount, -1)
}

// GetRefs returns the current reference count on the dispatch context.
func (t *RoutingContext) GetRefs() int32 {
	return atomic.LoadInt32(&t.refCount)
}

// New returns a new Dispatcher instance. The Dispatcher instance is initialized with an empty routing table.
func New(identityAttribute string, handlerGP *pool.GoroutinePool, enableTracing bool) *Dispatcher {
	return &Dispatcher{
		identityAttribute: identityAttribute,
		sessionPool:       newSessionPool(enableTracing),
		statePool:         newDispatchStatePool(),
		gp:                handlerGP,
		context: &RoutingContext{
			Routes: routing.Empty(),
		},
	}
}

// ChangeRoute changes the routing table on the Dispatcher which, in turn, ends up creating a new RoutingContext.
func (d *Dispatcher) ChangeRoute(new *routing.Table) *RoutingContext {
	newContext := &RoutingContext{
		Routes: new,
	}

	d.contextLock.Lock()
	old := d.context
	d.context = newContext
	d.contextLock.Unlock()

	return old
}

// Check implementation of runtime.Dispatcher.
func (d *Dispatcher) Check(ctx context.Context, bag attribute.Bag) (*adapter.CheckResult, error) {
	s := d.beginSession(ctx, tpb.TEMPLATE_VARIETY_CHECK, bag)

	var r *adapter.CheckResult
	err := d.dispatch(s)
	if err == nil {
		r = s.checkResult
		err = s.err
	}

	d.completeSession(s)

	return r, err
}

// Report implementation of runtime.Dispatcher.
func (d *Dispatcher) Report(ctx context.Context, bag attribute.Bag) error {
	s := d.beginSession(ctx, tpb.TEMPLATE_VARIETY_REPORT, bag)

	err := d.dispatch(s)
	if err == nil {
		err = s.err
	}

	d.completeSession(s)

	return err
}

// Quota implementation of runtime.Dispatcher.
func (d *Dispatcher) Quota(ctx context.Context, bag attribute.Bag, qma *runtime.QuotaMethodArgs) (*adapter.QuotaResult, error) {
	s := d.beginSession(ctx, tpb.TEMPLATE_VARIETY_QUOTA, bag)
	s.quotaMethodArgs = *qma

	err := d.dispatch(s)
	if err == nil {
		err = s.err
	}

	r := s.quotaResult
	d.completeSession(s)

	return r, err
}

// Preprocess implementation of runtime.Dispatcher.
func (d *Dispatcher) Preprocess(ctx context.Context, bag attribute.Bag, responseBag *attribute.MutableBag) error {
	s := d.beginSession(ctx, tpb.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR, bag)
	s.responseBag = responseBag

	err := d.dispatch(s)
	if err == nil {
		err = s.err
	}

	d.completeSession(s)

	return err
}

func (d *Dispatcher) dispatch(session *session) error {
	// Lookup the value of the identity attribute, so that we can extract the namespace to use for route
	// lookup.
	identityAttributeValue, err := getIdentityAttributeValue(session.bag, d.identityAttribute)
	if err != nil {
		// early return.
		updateRequestCounters(time.Since(session.start), 0, 0, err != nil)
		log.Warnf("unable to determine identity attribute value: '%v', operation='%d'", err, session.variety)
		return err
	}
	namespace := getNamespace(identityAttributeValue)

	// Capture the routing context locally. It can change underneath us. We also need to decRef before
	// completing the call.
	r := d.acquireRoutingContext()

	destinations := r.Routes.GetDestinations(session.variety, namespace)

	// Update the context after ensuring that we will not short-circuit and return. This causes allocations.
	session.ctx = d.updateContext(session.ctx, session.bag)

	// TODO(Issue #2139): This is for old-style metadata based policy decisions. This should be eventually removed.
	ctxProtocol, _ := session.bag.Get(config.ContextProtocolAttributeName)
	tcp := ctxProtocol == config.ContextProtocolTCP

	// Ensure that we can run dispatches to all destinations in parallel.
	session.ensureParallelism(destinations.Count())

	ninputs := 0
	ndestinations := 0
	for _, destination := range destinations.Entries() {
		for _, group := range destination.InstanceGroups {
			if !group.Matches(session.bag) || group.ResourceType.IsTCP() != tcp {
				continue
			}
			ndestinations++

			var state *dispatchState
			// We dispatch multiple instances together at once to a handler for report calls. pre-acquire
			// the state, so that we can use its instances field to stage the instance values before dispatch.
			if session.variety == tpb.TEMPLATE_VARIETY_REPORT {
				state = d.statePool.get(session, destination)
			}

			for j, input := range group.Builders {
				var instance interface{}
				if instance, err = input(session.bag); err != nil {
					log.Warnf("error creating instance: destination='%v', error='%v'", destination.FriendlyName, err)
					continue
				}
				ninputs++

				// For report templates, accumulate instances as much as possible before commencing dispatch.
				if session.variety == tpb.TEMPLATE_VARIETY_REPORT {
					state.instances = append(state.instances, instance)
					continue
				}

				// for other templates, dispatch for each instance individually.
				state = d.statePool.get(session, destination)
				state.instance = instance
				if session.variety == tpb.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR {
					state.mapper = group.Mappers[j]
				}

				// Dispatch for singleton dispatches
				d.dispatchToHandler(state)
			}

			if session.variety == tpb.TEMPLATE_VARIETY_REPORT {
				// Do a multi-instance dispatch for report.
				d.dispatchToHandler(state)
			}
		}
	}

	// wait on the dispatch states and accumulate results
	var buf *bytes.Buffer
	code := rpc.OK

	err = nil
	for session.activeDispatches > 0 {
		state := <-session.completed
		session.activeDispatches--

		// Aggregate errors
		if state.err != nil {
			err = multierror.Append(err, state.err)
		}

		st := rpc.Status{Code: int32(rpc.OK)}

		switch session.variety {
		case tpb.TEMPLATE_VARIETY_REPORT:
			// Do nothing

		case tpb.TEMPLATE_VARIETY_CHECK:
			if session.checkResult == nil {
				r := state.checkResult
				session.checkResult = &r
			} else {
				session.checkResult.CombineCheckResult(&state.checkResult)
			}
			st = state.checkResult.Status

		case tpb.TEMPLATE_VARIETY_QUOTA:
			if session.quotaResult == nil {
				r := state.quotaResult
				session.quotaResult = &r
			} else {
				log.Warnf("Skipping quota op result due to previous value: '%v', op: '%s'",
					state.quotaResult, state.destination.FriendlyName)
			}
			st = state.quotaResult.Status

		case tpb.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR:
			e := session.responseBag.Merge(state.outputBag)
			if e != nil {
				log.Infof("Attributes merging failed %v", err)
				err = e
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

		d.statePool.put(state)
	}

	if buf != nil {
		switch session.variety {
		case tpb.TEMPLATE_VARIETY_CHECK:
			session.checkResult.SetStatus(status.WithMessage(code, buf.String()))
		case tpb.TEMPLATE_VARIETY_QUOTA:
			session.quotaResult.SetStatus(status.WithMessage(code, buf.String()))
		}
		pool.PutBuffer(buf)
	}

	session.err = err

	r.DecRef()
	updateRequestCounters(time.Since(session.start), ndestinations, ninputs, err != nil)

	return nil
}

func (d *Dispatcher) acquireRoutingContext() *RoutingContext {
	d.contextLock.RLock()
	ctx := d.context
	ctx.IncRef()
	d.contextLock.RUnlock()
	return ctx
}

func (d *Dispatcher) updateContext(ctx context.Context, bag attribute.Bag) context.Context {
	data := &adapter.RequestData{}

	// fill the destination information
	if destSrvc, found := bag.Get(d.identityAttribute); found {
		data.DestinationService = adapter.Service{FullName: destSrvc.(string)}
	}

	return adapter.NewContextWithRequestData(ctx, data)
}

func (d *Dispatcher) beginSession(ctx context.Context, variety tpb.TemplateVariety, bag attribute.Bag) *session {
	s := d.sessionPool.get()
	s.start = time.Now()
	s.variety = variety
	s.ctx = ctx
	s.bag = bag

	return s
}

func (d *Dispatcher) completeSession(s *session) {
	s.clear()
	d.sessionPool.put(s)
}

func (d *Dispatcher) dispatchToHandler(s *dispatchState) {
	s.session.activeDispatches++

	d.gp.ScheduleWork(doDispatchToHandler, s)
}

func doDispatchToHandler(param interface{}) {
	s := param.(*dispatchState)

	reachedEnd := false

	defer func() {
		if reachedEnd {
			return
		}

		r := recover()
		s.err = fmt.Errorf("panic during handler dispatch: %v", r)
		log.Errorf("%v", s.err)

		if log.DebugEnabled() {
			log.Debugf("stack dump for handler dispatch panic:\n%s", debug.Stack())
		}

		s.session.completed <- s
	}()

	ctx := s.session.ctx
	var span opentracing.Span
	var start time.Time
	span, ctx, start = s.beginSpan(ctx)

	log.Debugf("begin dispatch: destination='%s'", s.destination.FriendlyName)

	switch s.destination.Template.Variety {
	case tpb.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR:
		s.outputBag, s.err = s.destination.Template.DispatchGenAttrs(
			ctx, s.destination.Handler, s.instance, s.inputBag, s.mapper)

	case tpb.TEMPLATE_VARIETY_CHECK:
		s.checkResult, s.err = s.destination.Template.DispatchCheck(
			ctx, s.destination.Handler, s.instance)

	case tpb.TEMPLATE_VARIETY_REPORT:
		s.err = s.destination.Template.DispatchReport(
			ctx, s.destination.Handler, s.instances)

	case tpb.TEMPLATE_VARIETY_QUOTA:
		s.quotaResult, s.err = s.destination.Template.DispatchQuota(
			ctx, s.destination.Handler, s.instance, s.quotaArgs)

	default:
		panic(fmt.Sprintf("unknown variety type: '%v'", s.destination.Template.Variety))
	}

	log.Debugf("complete dispatch: destination='%s' {err:%v}", s.destination.FriendlyName, s.err)

	s.completeSpan(span, time.Since(start), s.err)
	s.session.completed <- s

	reachedEnd = true
}

func (s *dispatchState) beginSpan(ctx context.Context) (opentracing.Span, context.Context, time.Time) {
	var span opentracing.Span
	if s.session.trace {
		span, ctx = opentracing.StartSpanFromContext(ctx, s.destination.FriendlyName)
	}

	return span, ctx, time.Now()
}

func (s *dispatchState) completeSpan(span opentracing.Span, duration time.Duration, err error) {
	if s.session.trace {
		logToDispatchSpan(span, s.destination.Template.Name, s.destination.HandlerName, s.destination.AdapterName, err)
	}
	s.destination.Counters.Update(duration, err != nil)
}
