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

	"istio.io/api/mixer/v1/template"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/aspect"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/pool"
	"istio.io/istio/mixer/pkg/runtime"
	"istio.io/istio/mixer/pkg/runtime2/routing"
)

type Dispatcher struct {
	destinationServiceAttribute string

	// the current, active table.
	table *routing.Table

	// the reader-writer lock for accessing or changing the table.
	routerLock sync.RWMutex

	// execPool pool for executing the dispatches in a vectorized manner
	execPool *executorPool
}

var _ runtime.Dispatcher = &Dispatcher{}

func New(destinationServiceAttribute string, handlerGP *pool.GoroutinePool) *Dispatcher {
	return &Dispatcher{
		destinationServiceAttribute: destinationServiceAttribute,
		execPool:                    newExecutorPool(handlerGP),
		table:                       routing.Empty(),
	}
}

func (d *Dispatcher) ChangeRoute(new *routing.Table) *routing.Table {
	d.routerLock.Lock()

	old := d.table
	d.table = new

	d.routerLock.Unlock()

	return old
}

func (d *Dispatcher) acquireRouter() *routing.Table {
	d.routerLock.RLock()
	r := d.table
	r.IncRef()
	d.routerLock.RUnlock()
	return r
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

func (d *Dispatcher) Check(ctx context.Context, bag attribute.Bag) (*adapter.CheckResult, error) {
	r := d.acquireRouter()
	ctx = d.updateContext(ctx, bag)

	destinations, err := r.GetDestinations(istio_mixer_v1_template.TEMPLATE_VARIETY_CHECK, bag)
	if err != nil {
		r.DecRef()
		return nil, err
	}

	if destinations.Count() == 0 {
		// early return
		r.DecRef()
		return nil, err
	}

	executor := d.execPool.get(destinations.Count())

	for _, destination := range destinations.Entries() {
		// TODO: avoid this allocation.
		instances := destination.BuildInstances(bag)
		for _, instance := range instances {
			// TODO:op
			executor.executeSingleInstance(ctx, destination.Handler, destination.Template.ProcessCheck2, nil, instance, "op")
		}
	}

	res, err := executor.wait()

	r.DecRef()
	return res.(*adapter.CheckResult), err // TODO: Validate that the cast is correct.
}

func (d *Dispatcher) Report(ctx context.Context, bag attribute.Bag) error {
	r := d.acquireRouter()
	ctx = d.updateContext(ctx, bag)

	destinations, err := r.GetDestinations(istio_mixer_v1_template.TEMPLATE_VARIETY_REPORT, bag)
	if err != nil {
		r.DecRef()
		return err
	}

	if destinations.Count() == 0 {
		// early return
		r.DecRef()
		return err
	}

	executor := d.execPool.get(destinations.Count())

	for _, destination := range destinations.Entries() {
		// TODO: avoid this allocation.
		instances := destination.BuildInstances(bag)
		executor.executeMultiInstance(ctx, destination.Handler, destination.Template.ProcessReport2, nil, instances, "op")
	}

	_, err = executor.wait()

	r.DecRef()
	return err
}

func (d *Dispatcher) Quota(ctx context.Context, bag attribute.Bag, qma *aspect.QuotaMethodArgs) (*adapter.QuotaResult, error) {
	r := d.acquireRouter()
	ctx = d.updateContext(ctx, bag)

	destinations, err := r.GetDestinations(istio_mixer_v1_template.TEMPLATE_VARIETY_QUOTA, bag)
	if err != nil {
		r.DecRef()
		return nil, err
	}

	if destinations.Count() == 0 {
		// early return
		r.DecRef()
		return nil, err
	}

	executor := d.execPool.get(destinations.Count())

	for _, destination := range destinations.Entries() {
		// TODO: avoid this allocation.
		instances := destination.BuildInstances(bag)
		executor.executeMultiInstance(ctx, destination.Handler, destination.Template.ProcessQuota2, nil, instances, "op")
	}

	res, err := executor.wait()

	r.DecRef()

	if err != nil {
		return nil, err
	}
	return res.(*adapter.QuotaResult), err // TODO: Validate that the cast is correct.
}

func (d *Dispatcher) Preprocess(ctx context.Context, bag attribute.Bag, responseBag *attribute.MutableBag) error {
	r := d.acquireRouter()
	ctx = d.updateContext(ctx, bag)

	destinations, err := r.GetDestinations(istio_mixer_v1_template.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR, bag)
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
		// TODO: avoid this allocation.
		instances := destination.BuildInstances(bag)
		executor.executeMultiInstance(ctx, destination.Handler, destination.Template.ProcessGenAttrs2, nil, instances, "op")
	}

	_, err = executor.wait()

	r.DecRef()

	return err
}
