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

package routing

import (
	"sync/atomic"

	"istio.io/api/mixer/v1/template"
	"istio.io/istio/mixer/pkg/attribute"
)

var emptySet = &DestinationSet{
	entries: []Destination{},
}

// Table calculates the dispatch varietyDestinations for an incoming request.
type Table struct {

	// destinations grouped by variety
	entries map[istio_mixer_v1_template.TemplateVariety]*VarietyDestinations

	// identityAttribute defines which configuration scopes apply to a request.
	// default: target.service
	// The value of this attribute is expected to be a hostname of form "svc.$ns.suffix"
	identityAttribute string

	// defaultConfigNamespace defines the namespace that contains configuration defaults for istio.
	// This is distinct from the "default" namespace in K8s.
	// default: istio-default-config
	defaultConfigNamespace string

	// the current reference count. Indicates how-many calls are currently active.
	refCount int32
}

type VarietyDestinations struct {
	// destinations grouped by namespace
	entries map[string]*DestinationSet

	// destinations for default namespace
	defaultSet *DestinationSet
}

type DestinationSet struct {
	entries []Destination
}

func (r *Table) IncRef() {
	atomic.AddInt32(&r.refCount, 1)
}

func (r *Table) DecRef() {
	atomic.AddInt32(&r.refCount, -1)
}

func (r *Table) GetDestinations(variety istio_mixer_v1_template.TemplateVariety, bag attribute.Bag) (*DestinationSet, error) {
	destinations, ok := r.entries[variety]
	if !ok {
		// TODO: log
		return emptySet, nil
	}

	return destinations.getDestinations(bag, r.identityAttribute)
}

func (v *VarietyDestinations) getDestinations(attributes attribute.Bag, idAttribute string) (*DestinationSet, error) {
	destination, err := getDestination(attributes, idAttribute)
	if err != nil {
		// TODO: log
		return nil, err
	}

	namespace := getNamespace(destination)

	nsSet := v.entries[namespace]
	if nsSet == nil {
		nsSet = v.defaultSet
	}

	return nsSet, nil
}

func (v *DestinationSet) Count() int {
	return len(v.entries)
}

func (v *DestinationSet) Entries() []Destination {
	return v.entries
}
