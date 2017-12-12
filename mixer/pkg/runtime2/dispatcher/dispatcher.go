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
	"errors"
	"fmt"
	"strings"

	"istio.io/api/mixer/v1/template"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/il/compiled"
	"istio.io/istio/mixer/pkg/log"
	"istio.io/istio/mixer/pkg/template"
)

// Dispatcher contains dispatcher targets for efficient dispatching of incoming requests.
type Dispatcher struct {

	// targets by template variety.
	targets map[istio_mixer_v1_template.TemplateVariety]*VarietyTargets

	// identityAttribute defines which configuration scopes apply to a request.
	// default: target.service
	// The value of this attribute is expected to be a hostname of form "svc.$ns.suffix"
	identityAttribute string

	// defaultConfigNamespace defines the namespace that contains configuration defaults for istio.
	// This is distinct from the "default" namespace in K8s.
	// default: istio-default-config
	defaultConfigNamespace string

	// executor pool for executing the dispatches in a vectorized manner
	executor *VectoredExecutorPool
}

// VarietyTargets contains targets for a particular variaty.
type VarietyTargets struct {
	// targets by namespace
	byNamespace map[string]TargetSet

	// byNamespace in the default namespace
	defaultTargets TargetSet
}

// TargetSet has a set of targets that conform to a particular rule (i.e. within the same namespace).
type TargetSet struct {
	dispatchers []DispatchTarget
}

// DispatchTarget conditionally dispatches an incoming request to a template-constructed template.Processor.
type DispatchTarget struct {
	condition compiled.Expression
	processor template.Processor
}

func (t *Dispatcher) DispatchReport(ctx context.Context, bag attribute.Bag) (adapter.Result, error) {
	return t.dispatch(ctx, istio_mixer_v1_template.TEMPLATE_VARIETY_REPORT, bag)
}

//func (t *Dispatcher) DispatchCheck(ctx context.Context, bag attribute.Bag) error {
//	return t.dispatch(ctx, istio_mixer_v1_template.TEMPLATE_VARIETY_CHECK, bag, doCheck)
//}

func (d *Dispatcher) dispatch(ctx context.Context, variety istio_mixer_v1_template.TemplateVariety, bag attribute.Bag) (adapter.Result, error) {

	byVariety, ok := d.targets[variety]
	if !ok {
		// TODO: NYI
		panic("nyi")
	}

	defaultSet, namespaceSet, err := d.getDispatchSets(byVariety, bag)

	if err != nil {
		// TODO: NYI
		panic("nyi")
	}

	// Get an executor from the pool
	executor := d.executor.get(defaultSet.Size() + namespaceSet.Size())

	// dispatch the default target set, and the namespace-specific set.
	defaultSet.dispatch(executor, ctx, bag)
	namespaceSet.dispatch(executor, ctx, bag)

	// aggregate and combine the results and return back.
	result, err := executor.AggregateResults()

	d.executor.put(executor)

	return result, err
}

func (s TargetSet) dispatch(e *VectoredExecutor, ctx context.Context, bag attribute.Bag) error {
	if s.dispatchers == nil {
		return nil
	}

	for _, d := range s.dispatchers {
		if d.condition != nil {
			match, err := d.condition.EvaluateBoolean(bag)
			if err != nil {
				// TODO: NYI
				panic("nyi")
			}
			if !match {
				continue
			}
		}

		// TODO: op
		e.Invoke(ctx, bag, d.processor, "op")
	}

	return nil
}

func (s TargetSet) Size() int {
	if s.dispatchers == nil {
		return 0
	}

	return len(s.dispatchers)
}

func (s Dispatcher) getDispatchSets(byVariety *VarietyTargets, bag attribute.Bag) (TargetSet, TargetSet, error) {
	dest, err := getDestination(bag, s.identityAttribute)
	if err != nil {
		// TODO: NYI
		panic("NYI")
	}

	ns := getNamespace(dest)

	if ns == s.defaultConfigNamespace {
		return byVariety.defaultTargets, TargetSet{}, nil
	}

	if nsDispatchers, found := byVariety.byNamespace[ns]; found {
		return byVariety.defaultTargets, nsDispatchers, nil
	}

	return byVariety.defaultTargets, TargetSet{}, nil
}


// getDestination from the attribute bag, based on the id attribute.
func getDestination(attrs attribute.Bag, idAttribute string) (string, error) {

	v, ok := attrs.Get(idAttribute)
	if !ok {
		msg := fmt.Sprintf("%s identity not found in attributes%v", idAttribute, attrs.Names())
		log.Warnf(msg)
		return "", errors.New(msg)
	}

	var destination string
	if destination, ok = v.(string); !ok {
		msg := fmt.Sprintf("%s identity must be string: %v", idAttribute, v)
		log.Warnf(msg)
		return "", errors.New(msg)
	}

	return destination, nil
}

func getNamespace(destination string) string {

	// TODO: This is a potential garbage source on hot path. Consider fixing it.
	splits := strings.SplitN(destination, ".", 3) // we only care about service and namespace.
	if len(splits) > 1 {
		return splits[1]
	}
	return ""
}

