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

// Router calculates the dispatch destinations for an incoming request and dispatches the requests to the appropriate
// handlers on a scatter-gather execPool that protects against panics.
type Router struct {

	// destinations by template variety.
	destinations map[istio_mixer_v1_template.TemplateVariety]*varietyDestinations

	// identityAttribute defines which configuration scopes apply to a request.
	// default: target.service
	// The value of this attribute is expected to be a hostname of form "svc.$ns.suffix"
	identityAttribute string

	// defaultConfigNamespace defines the namespace that contains configuration defaults for istio.
	// This is distinct from the "default" namespace in K8s.
	// default: istio-default-config
	defaultConfigNamespace string

	// execPool pool for executing the dispatches in a vectorized manner
	execPool *executorPool
}

// destinations for a specific variety.
type varietyDestinations struct {

	// destinations by namespace
	byNamespace map[string]destinationSet

	// destinations for the default config namespace
	defaultTargets destinationSet
}

// destinationSet has a set of destinations that conform to a particular rule (i.e. within the same namespace).
type destinationSet struct {
	destinations []destination
}

// destination conditionally dispatches an incoming request to a template-constructed template.Processor.
type destination struct {
	condition compiled.Expression
	processor template.Processor
}

func (t *Router) DispatchReport(ctx context.Context, bag attribute.Bag) (adapter.Result, error) {
	return t.dispatch(ctx, istio_mixer_v1_template.TEMPLATE_VARIETY_REPORT, bag)
}

//func (t *Router) DispatchCheck(ctx context.Context, bag attribute.Bag) error {
//	return t.dispatch(ctx, istio_mixer_v1_template.TEMPLATE_VARIETY_CHECK, bag, doCheck)
//}

func (d *Router) dispatch(ctx context.Context, variety istio_mixer_v1_template.TemplateVariety, bag attribute.Bag) (adapter.Result, error) {

	byVariety, ok := d.destinations[variety]
	if !ok {
		// TODO: NYI
		panic("nyi")
	}

	defaultSet, namespaceSet, err := d.getDestinationSets(byVariety, bag)

	if err != nil {
		// TODO: NYI
		panic("nyi")
	}

	// Get an executor from the pool
	executor := d.execPool.get(defaultSet.Size() + namespaceSet.Size())

	// dispatch the default target set, and the namespace-specific set.
	defaultSet.dispatch(executor, ctx, bag)
	namespaceSet.dispatch(executor, ctx, bag)

	// aggregate and combine the results and return back.
	result, err := executor.wait()

	d.execPool.put(executor)

	return result, err
}

func (s destinationSet) dispatch(e *executor, ctx context.Context, bag attribute.Bag) error {
	if s.destinations == nil {
		return nil
	}

	for _, d := range s.destinations {
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
		e.execute(ctx, bag, d.processor, "op")
	}

	return nil
}

func (s destinationSet) Size() int {
	if s.destinations == nil {
		return 0
	}

	return len(s.destinations)
}

func (s Router) getDestinationSets(byVariety *varietyDestinations, bag attribute.Bag) (destinationSet, destinationSet, error) {
	dest, err := getDestination(bag, s.identityAttribute)
	if err != nil {
		// TODO: NYI
		panic("NYI")
	}

	ns := getNamespace(dest)

	if ns == s.defaultConfigNamespace {
		return byVariety.defaultTargets, destinationSet{}, nil
	}

	if nsDispatchers, found := byVariety.byNamespace[ns]; found {
		return byVariety.defaultTargets, nsDispatchers, nil
	}

	return byVariety.defaultTargets, destinationSet{}, nil
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

