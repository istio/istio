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

package dispatcher

import (
	"context"
	"sync"
	"sync/atomic"

	"istio.io/api/mixer/v1/template"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/aspect"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/pool"
	"istio.io/istio/mixer/pkg/runtime"
	"istio.io/istio/mixer/pkg/runtime2/routing"
	"istio.io/istio/pkg/log"
)

// Dispatcher is the runtime2 implementation of the runtime.Dispatcher interface.
type Dispatcher struct {
	destinationServiceAttribute string

	// Current dispatch context.
	context *DispatchContext

	// the reader-writer lock for accessing or changing the context.
	routerLock sync.RWMutex

	// execPool pool for executing the dispatches in a vectorized manner
	execPool *executorPool
}

// DispatchContext is the current dispatching context, based on a config snapshot. As config changes, the current/live
// DispatchContext also changes.
type DispatchContext struct {
	// the current, active Routes.
	Routes *routing.Table

	// the current reference count. Indicates how-many calls are currently using this DispatchContext.
	refCount int32
}

// IncRef increases the reference count on the DispatchContext.
func (t *DispatchContext) IncRef() {
	atomic.AddInt32(&t.refCount, 1)
}

// DecRef decreases the reference count on the DispatchContext.
func (t *DispatchContext) DecRef() {
	atomic.AddInt32(&t.refCount, -1)
}

// GetRefs returns the current reference count on the dispatch context.
func (t *DispatchContext) GetRefs() int32 {
	return atomic.LoadInt32(&t.refCount)
}

var _ runtime.Dispatcher = &Dispatcher{}

// New returns a new Dispatcher instance. The Dispatcher instance is initialized with an empty routing table.
func New(destinationServiceAttribute string, handlerGP *pool.GoroutinePool) *Dispatcher {
	return &Dispatcher{
		destinationServiceAttribute: destinationServiceAttribute,
		execPool:                    newExecutorPool(handlerGP),
		context: &DispatchContext{
			Routes: routing.Empty(),
		},
	}
}

// ChangeRoute changes the routing table on the Dispatcher which, in turn, ends up creating a new DispatchContext.
func (d *Dispatcher) ChangeRoute(new *routing.Table) *DispatchContext {
	d.routerLock.Lock()

	old := d.context

	newContext := &DispatchContext{
		Routes: new,
	}
	d.context = newContext

	d.routerLock.Unlock()

	return old
}

func (d *Dispatcher) acquireContext() *DispatchContext {
	d.routerLock.RLock()
	ctx := d.context
	ctx.IncRef()
	d.routerLock.RUnlock()
	return ctx
}

func (d *Dispatcher) updateContext(ctx context.Context, bag attribute.Bag) context.Context {
	//}, destinationServiceAttr string) context.Context {
	data := &adapter.RequestData{}

	// fill the destination information
	if destSrvc, found := bag.Get(d.destinationServiceAttribute); found {
		data.DestinationService = adapter.Service{FullName: destSrvc.(string)}
	}

	return adapter.NewContextWithRequestData(ctx, data)
}

// Check implementation of runtime.Dispatcher.
func (d *Dispatcher) Check(ctx context.Context, bag attribute.Bag) (*adapter.CheckResult, error) {
	c := d.acquireContext()
	ctx = d.updateContext(ctx, bag)

	destinations, err := c.Routes.GetDestinations(istio_mixer_v1_template.TEMPLATE_VARIETY_CHECK, bag)
	if err != nil {
		c.DecRef()
		return nil, err
	}

	if destinations.Count() == 0 {
		// early return
		c.DecRef()
		return nil, err
	}

	executor := d.execPool.get(destinations.Count())

	for _, destination := range destinations.Entries() {
		for _, inputs := range destination.Inputs {
			if !inputs.ShouldApply(bag) {
				continue
			}

			for _, input := range inputs.Builders {
				var instance interface{}
				if instance, err = input(bag); err != nil {
					// TODO: Better logging.
					log.Warnf("Unable to create instance: '%v'", err)
					continue
				}
				executor.executeCheck(ctx, destination.Template.ProcessCheck2, destination.Handler, instance)
			}
		}
	}

	res, err := executor.wait()

	c.DecRef()
	return res.(*adapter.CheckResult), err // TODO: Validate that the cast is correct.
}

// Report implementation of runtime.Dispatcher.
func (d *Dispatcher) Report(ctx context.Context, bag attribute.Bag) error {
	c := d.acquireContext()
	ctx = d.updateContext(ctx, bag)

	destinations, err := c.Routes.GetDestinations(istio_mixer_v1_template.TEMPLATE_VARIETY_REPORT, bag)
	if err != nil {
		c.DecRef()
		return err
	}

	if destinations.Count() == 0 {
		// early return
		c.DecRef()
		return err
	}

	executor := d.execPool.get(destinations.Count())

	for _, destination := range destinations.Entries() {
		// TODO: We can create a pooled buffer to avoid this allocation
		instances := make([]interface{}, 0, destination.MaxInstances())
		for _, inputs := range destination.Inputs {
			if !inputs.ShouldApply(bag) {
				continue
			}
			for _, input := range inputs.Builders {
				var instance interface{}
				if instance, err = input(bag); err != nil {
					// TODO: Better logging.
					log.Warnf("Unable to create instance: '%v'", err)
					continue
				}
				instances = append(instances, instance)
			}
		}
		executor.executeReport(ctx, destination.Template.ProcessReport2, destination.Handler, instances)
	}

	_, err = executor.wait()

	c.DecRef()
	return err
}

// Quota implementation of runtime.Dispatcher.
func (d *Dispatcher) Quota(ctx context.Context, bag attribute.Bag, qma *aspect.QuotaMethodArgs) (*adapter.QuotaResult, error) {
	c := d.acquireContext()
	ctx = d.updateContext(ctx, bag)

	destinations, err := c.Routes.GetDestinations(istio_mixer_v1_template.TEMPLATE_VARIETY_QUOTA, bag)
	if err != nil {
		c.DecRef()
		return nil, err
	}

	if destinations.Count() == 0 {
		// early return
		c.DecRef()
		return nil, err
	}

	quotaArgs := adapter.QuotaArgs{
		DeduplicationID: qma.DeduplicationID,
		QuotaAmount:     qma.Amount,
		BestEffort:      qma.BestEffort,
	}

	executor := d.execPool.get(destinations.Count())

	for _, destination := range destinations.Entries() {
		for _, inputs := range destination.Inputs {
			if !inputs.ShouldApply(bag) {
				continue
			}

			for _, input := range inputs.Builders {
				var instance interface{}
				if instance, err = input(bag); err != nil {
					// TODO: Better logging.
					log.Warnf("Unable to create instance: '%v'", err)
					continue
				}

				executor.executeQuota(ctx, destination.Template.ProcessQuota2, destination.Handler, quotaArgs, instance)
			}
		}
	}

	res, err := executor.wait()

	c.DecRef()

	if err != nil {
		return nil, err
	}
	return res.(*adapter.QuotaResult), err // TODO: Validate that the cast is correct.
}

// Preprocess implementation of runtime.Dispatcher.
func (d *Dispatcher) Preprocess(ctx context.Context, bag attribute.Bag, responseBag *attribute.MutableBag) error {
	r := d.acquireContext()
	ctx = d.updateContext(ctx, bag)

	destinations, err := r.Routes.GetDestinations(istio_mixer_v1_template.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR, bag)
	if err != nil {
		r.DecRef()
		return err
	}

	if destinations.Count() == 0 {
		// early return
		r.DecRef()
		return nil
	}

	executor := d.execPool.get(destinations.Count())

	for _, destination := range destinations.Entries() {
		for _, inputs := range destination.Inputs {
			if !inputs.ShouldApply(bag) {
				continue
			}

			for i, input := range inputs.Builders {
				var instance interface{}
				instance, err = input(bag)
				if err != nil {
					// TODO: Better logging.
					log.Warnf("Unable to create instance: '%v'", err)
					continue
				}
				executor.executePreprocess(ctx, destination.Template.ProcessGenAttrs2, destination.Handler, instance, bag, inputs.Mappers[i])
			}
		}
	}

	_, err = executor.wait()

	r.DecRef()

	return err
}
