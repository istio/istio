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
	"fmt"
	"runtime/debug"
	"time"

	opentracing "github.com/opentracing/opentracing-go"

	tpb "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/runtime/routing"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/istio/pkg/log"
)

// dispatchState keeps the input/output state during the dispatch to a handler. It is used as temporary
// memory location to keep ephemeral state, thus avoiding garbage creation.
type dispatchState struct {
	session *session
	ctx     context.Context

	destination *routing.Destination
	mapper      template.OutputMapperFn

	inputBag   attribute.Bag
	quotaArgs  adapter.QuotaArgs
	instances  []interface{}
	actionName string

	// output state that was collected from the handler.
	err         error
	outputBag   *attribute.MutableBag
	checkResult adapter.CheckResult
	checkOutput interface{}
	quotaResult adapter.QuotaResult
}

func (ds *dispatchState) clear() {
	ds.session = nil
	ds.ctx = nil
	ds.destination = nil
	ds.mapper = nil
	ds.inputBag = nil
	ds.quotaArgs = adapter.QuotaArgs{}
	ds.err = nil
	ds.outputBag = nil
	ds.checkResult = adapter.CheckResult{}
	ds.checkOutput = nil
	ds.quotaResult = adapter.QuotaResult{}

	// re-slice to change the length to 0 without changing capacity.
	ds.instances = ds.instances[:0]
}

func (ds *dispatchState) beginSpan(ctx context.Context) (opentracing.Span, context.Context, time.Time) {
	var span opentracing.Span
	if ds.session.impl.enableTracing {
		span, ctx = opentracing.StartSpanFromContext(ctx, ds.destination.FriendlyName)
	}

	return span, ctx, time.Now()
}

func (ds *dispatchState) completeSpan(span opentracing.Span, duration time.Duration, err error) {
	if ds.session.impl.enableTracing {
		logToDispatchSpan(span, ds.destination.Template.Name, ds.destination.HandlerName, ds.destination.AdapterName, err)
		span.Finish()
	}
	ds.destination.Counters.Update(duration, err != nil)
}

func (ds *dispatchState) invokeHandler(interface{}) {
	reachedEnd := false

	defer func() {
		if reachedEnd {
			return
		}

		r := recover()
		ds.err = fmt.Errorf("panic during handler dispatch: %v", r)
		log.Errorf("%v\n%s", ds.err, debug.Stack())

		if log.DebugEnabled() {
			log.Debugf("stack dump for handler dispatch panic:\n%s", debug.Stack())
		}

		ds.session.completed <- ds
	}()

	span, ctx, start := ds.beginSpan(ds.ctx)

	log.Debugf("begin dispatch: destination='%s', variety=%q", ds.destination.FriendlyName, ds.destination.Template.Variety)

	switch ds.destination.Template.Variety {
	case tpb.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR:
		ds.outputBag, ds.err = ds.destination.Template.DispatchGenAttrs(
			ctx, ds.destination.Handler, ds.instances[0], ds.inputBag, ds.mapper)

	case tpb.TEMPLATE_VARIETY_CHECK:
		ds.checkResult, ds.err = ds.destination.Template.DispatchCheck(
			ctx, ds.destination.Handler, ds.instances[0])

	case tpb.TEMPLATE_VARIETY_CHECK_WITH_OUTPUT:
		ds.checkResult, ds.checkOutput, ds.err = ds.destination.Template.DispatchCheckOutput(
			ctx, ds.destination.Handler, ds.instances[0])

	case tpb.TEMPLATE_VARIETY_REPORT:
		ds.err = ds.destination.Template.DispatchReport(
			ctx, ds.destination.Handler, ds.instances)

	case tpb.TEMPLATE_VARIETY_QUOTA:
		ds.quotaResult, ds.err = ds.destination.Template.DispatchQuota(
			ctx, ds.destination.Handler, ds.instances[0], ds.quotaArgs)

	default:
		panic(fmt.Sprintf("unknown variety type: '%v'", ds.destination.Template.Variety))
	}

	log.Debugf("complete dispatch: destination='%s' {err:%v}", ds.destination.FriendlyName, ds.err)

	ds.completeSpan(span, time.Since(start), ds.err)
	ds.session.completed <- ds

	reachedEnd = true
}
