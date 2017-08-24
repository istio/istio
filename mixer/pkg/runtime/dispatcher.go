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

package runtime

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/glog"
	rpc "github.com/googleapis/googleapis/google/rpc"
	multierror "github.com/hashicorp/go-multierror"
	opentracing "github.com/opentracing/opentracing-go"
	tracelog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/client_golang/prometheus"

	"istio.io/mixer/pkg/adapter"
	adptTmpl "istio.io/mixer/pkg/adapter/template"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	cpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/pool"
	"istio.io/mixer/pkg/status"
	"istio.io/mixer/pkg/template"
)

// Dispatcher dispatches incoming API calls to configured adapters.
type Dispatcher interface {
	// Preprocess dispatches to the set of adapters that will run before any
	// other adapters in Mixer (aka: the Check, Report, Quota adapters).
	Preprocess(ctx context.Context, requestBag attribute.Bag, responseBag *attribute.MutableBag) error

	// Check dispatches to the set of adapters associated with the Check API method
	Check(ctx context.Context, requestBag attribute.Bag) (*adapter.CheckResult, error)

	// Report dispatches to the set of adapters associated with the Report API method
	Report(ctx context.Context, requestBag attribute.Bag) error

	// Quota dispatches to the set of adapters associated with the Quota API method
	Quota(ctx context.Context, requestBag attribute.Bag,
		qma *aspect.QuotaMethodArgs) (*adapter.QuotaResult2, error)
}

// Resolver represents the current snapshot of the configuration database
// and associated, initialized handlers.
type Resolver interface {
	// Resolve resolves configuration to a list of actions.
	// The result is encapsulated in the Actions interface.
	Resolve(bag attribute.Bag, variety adptTmpl.TemplateVariety) (Actions, error)
}

// Actions combines []*Action with a lifecycle (Done) function.
type Actions interface {
	// Get gets the encapsulated actions.
	Get() []*Action

	// Done is used by the caller to indicate that
	// the resolved actions will not be used further.
	// This can be used for reference counting.
	Done()
}

// Action is the runtime representation of a configured action - cpb.Action.
// Configuration is processed to hydrate instance names to Instances and handler.
type Action struct {
	// generated code - processor.ProcessXXX ()
	processor *template.Info
	// ready to use handler
	handler adapter.Handler
	// Name of the handler being called. Informational.
	handlerName string
	// Name of adapter that created the handler. Informational.
	adapterName string
	// handler to call.
	// instanceConfigs to dispatch to the handler.
	// instanceConfigs must belong to the same template.
	instanceConfig []*cpb.Instance
}

// genDispatchFn creates dispatchFn closures based on the given action.
type genDispatchFn func(call *Action) []dispatchFn

// newDispatcher creates a new dispatcher.
func newDispatcher(mapper expr.Evaluator, rt Resolver, gp *pool.GoroutinePool) *dispatcher {
	m := &dispatcher{
		mapper: mapper,
		gp:     gp,
	}
	m.ChangeResolver(rt)
	return m
}

// dispatcher is responsible for dispatching incoming API calls
// to the configured adapters. It implements the Dispatcher interface.
type dispatcher struct {
	// mapper is the selector and expression evaluator.
	// It is not directly used by dispatcher.
	mapper expr.Evaluator

	// gp is used to dispatch multiple adapters concurrently.
	gp *pool.GoroutinePool

	resolverLock sync.RWMutex
	resolver     Resolver
}

// ChangeResolver installs a new resolver.
// This function is called when configuration is updated by the user.
// oldResolver is returned so that it can be reclaimed.
func (m *dispatcher) ChangeResolver(rt Resolver) {
	m.resolverLock.Lock()
	m.resolver = rt
	m.resolverLock.Unlock()
}

// Resolve resolves configuration to a list of actions.
func (m *dispatcher) Resolve(bag attribute.Bag, variety adptTmpl.TemplateVariety) (Actions, error) {
	m.resolverLock.RLock()
	// Ensure that the lock is released even if resolver.Resolve panics.
	defer m.resolverLock.RUnlock()

	// resolver.Resolve is called under a readLock so that all
	// in-flight actions are correctly ref counted during a config change.
	// actions.Done() from every outstanding action indicates that the
	// configuration is no longer in use.
	actions, err := m.resolver.Resolve(bag, variety)
	return actions, err
}

// dispatch dispatches to functions generated by the genDispatchFn
func (m *dispatcher) dispatch(ctx context.Context, requestBag attribute.Bag, variety adptTmpl.TemplateVariety,
	genDispatchFn genDispatchFn) (adapter.Result, error) {
	calls, err := m.Resolve(requestBag, variety)
	if err != nil {
		glog.Error(err)
		return nil, err
	}

	// This *must* run in order to ensure proper cleanup.
	// It must run *after* all the processing is done.
	// Defer guarantees both.
	defer calls.Done()

	if glog.V(2) {
		glog.Infof("Resolved (%v) %d actions", variety, len(calls.Get()))
	}

	ra := make([]*runArg, 0, len(calls.Get()))
	for _, call := range calls.Get() {
		for _, df := range genDispatchFn(call) {
			ra = append(ra, &runArg{
				call,
				df,
			})
		}
	}
	return m.run(ctx, ra)
}

// Report dispatches to the set of adapters associated with the Report API method
// Config validation ensures that things are consistent.
// If they are not, we should continue as far as possible on the runtime path
// before aborting. Returns an error if any of the adapters return an error.
// Dispatcher#Report.
func (m *dispatcher) Report(ctx context.Context, requestBag attribute.Bag) error {
	_, err := m.dispatch(ctx, requestBag, adptTmpl.TEMPLATE_VARIETY_REPORT,
		func(call *Action) []dispatchFn {
			instCfg := make(map[string]proto.Message)
			for _, inst := range call.instanceConfig {
				instCfg[inst.Name] = inst.Params.(proto.Message)
			}
			return []dispatchFn{func(ctx context.Context) *result {
				err := call.processor.ProcessReport(ctx, instCfg, requestBag, m.mapper, call.handler)
				return &result{err: err, callinfo: call}
			}}
		},
	)
	return err
}

// Check dispatches to the set of adapters associated with the Check API method
// Config validation ensures that things are consistent.
// If they are not, we should continue as far as possible on the runtime path
// before aborting. Returns an error if any of the adapters return an error.
// If not the results are combined to a single CheckResult.
// Dispatcher#Check.
func (m *dispatcher) Check(ctx context.Context, requestBag attribute.Bag) (*adapter.CheckResult, error) {
	cres, err := m.dispatch(ctx, requestBag, adptTmpl.TEMPLATE_VARIETY_CHECK,
		func(call *Action) []dispatchFn {
			ra := make([]dispatchFn, 0, len(call.instanceConfig))
			for _, inst := range call.instanceConfig {
				ra = append(ra,
					func(ctx context.Context) *result {
						resp, err := call.processor.ProcessCheck(ctx, inst.Name,
							inst.Params.(proto.Message),
							requestBag, m.mapper,
							call.handler)
						return &result{err, &resp, call}
					})
			}
			return ra
		},
	)
	res, _ := cres.(*adapter.CheckResult)
	return res, err
}

// Quota dispatches to the set of adapters associated with the Quota API method
// Config validation ensures that things are consistent.
// Quota calls are dispatched to at most one handler.
// Dispatcher#Quota.
func (m *dispatcher) Quota(ctx context.Context, requestBag attribute.Bag,
	qma *aspect.QuotaMethodArgs) (*adapter.QuotaResult2, error) {
	dispatched := false
	qres, err := m.dispatch(ctx, requestBag, adptTmpl.TEMPLATE_VARIETY_QUOTA,
		func(call *Action) []dispatchFn {
			for _, inst := range call.instanceConfig {
				if inst.Name != qma.Quota {
					continue
				}
				if dispatched { // ensures only one call is dispatched.
					glog.Warningf("Multiple dispatch: not dispatching %s to handler %s", inst.Name, call.handlerName)
					return nil
				}
				dispatched = true
				return []dispatchFn{
					func(ctx context.Context) *result {
						resp, err := call.processor.ProcessQuota(ctx, inst.Name,
							inst.Params.(proto.Message), requestBag, m.mapper, call.handler,
							adapter.QuotaRequestArgs{
								DeduplicationID: qma.DeduplicationID,
								QuotaAmount:     qma.Amount,
								BestEffort:      qma.BestEffort,
							})
						return &result{err, &resp, call}
					},
				}
			}
			return nil
		},
	)
	res, _ := qres.(*adapter.QuotaResult2)
	return res, err
}

// Preprocess runs the first phase of adapter processing before any other adapters are run.
// Attribute producing adapters are run in this phase.
func (m *dispatcher) Preprocess(ctx context.Context, requestBag attribute.Bag, responseBag *attribute.MutableBag) error {
	// FIXME
	return errors.New("not implemented")
}

// combineResults combines results
func combineResults(results []*result) (adapter.Result, error) {
	var res adapter.Result
	var err *multierror.Error
	var buf *bytes.Buffer
	code := rpc.OK

	for _, rs := range results {
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
				code = rpc.Code(st.Code)
			} else {
				buf.WriteString(", ")
			}

			buf.WriteString(rs.callinfo.handlerName + ":" + st.Message)
		}
	}

	if buf != nil {
		res.SetStatus(status.WithMessage(code, buf.String()))
		pool.PutBuffer(buf)
	}
	return res, err.ErrorOrNil()
}

// dispatchFn is the abstraction used by runAsync to dispatch to adapters.
type dispatchFn func(context.Context) *result

// result encapsulates commonalities between all adapter returns.
type result struct {
	// all results return an error
	err error
	// CheckResult or QuotaResult
	res adapter.Result
	// callinfo that resulted in "res". Used for informational purposes.
	callinfo *Action
}

// runArg encapsulates callinfo with the dispatchFn that acts on it.
type runArg struct {
	callinfo *Action
	dispatch dispatchFn
}

// run runArgs using runAsync and return results.
func (m *dispatcher) run(ctx context.Context, runArgs []*runArg) (adapter.Result, error) {
	nresults := len(runArgs)
	resultsChan := make(chan *result, nresults)
	results := make([]*result, nresults)

	for _, ra := range runArgs {
		m.runAsync(ctx, ra.callinfo, resultsChan, ra.dispatch)
	}

	for i := 0; i < nresults; i++ {
		results[i] = <-resultsChan
	}
	return combineResults(results)
}

// runAsync runs the dispatchFn using a scheduler. It also adds a new span and records prometheus metrics.
func (m *dispatcher) runAsync(ctx context.Context, callinfo *Action, results chan *result, do dispatchFn) {
	m.gp.ScheduleWork(func() {
		// tracing
		op := callinfo.processor.Name + ":" + callinfo.handlerName + "(" + callinfo.adapterName + ")"
		span, ctx := opentracing.StartSpanFromContext(ctx, op)
		start := time.Now()
		out := do(ctx)

		st := status.OK
		if out.err != nil {
			st = status.WithError(out.err)
		}

		duration := time.Since(start)
		span.LogFields(
			tracelog.String(meshFunction, callinfo.processor.Name),
			tracelog.String(handlerName, callinfo.handlerName),
			tracelog.String(adapterName, callinfo.adapterName),
			tracelog.String(responseCode, rpc.Code_name[st.Code]),
			tracelog.String(responseMsg, st.Message),
			tracelog.Bool(errorStr, out.err != nil),
		)

		dispatchLbls := prometheus.Labels{
			meshFunction: callinfo.processor.Name,
			handlerName:  callinfo.handlerName,
			adapterName:  callinfo.adapterName,
			responseCode: rpc.Code_name[st.Code],
			errorStr:     strconv.FormatBool(out.err != nil),
		}
		dispatchCounter.With(dispatchLbls).Inc()
		dispatchDuration.With(dispatchLbls).Observe(duration.Seconds())

		results <- out
		span.Finish()
	})
}
