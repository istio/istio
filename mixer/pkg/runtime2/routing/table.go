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
	"istio.io/api/mixer/v1/template"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/il/compiled"
	"istio.io/istio/mixer/pkg/runtime2/config"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/istio/pkg/log"
)

var emptySet = &HandlerEntries{
	entries: []*HandlerEntry{},
}

// Table is the main routing table. It is used to find the set of handlers that should be invoked, along with the,
// instance builders and match conditions.
type Table struct {
	// ID of the table. This is based on the config snapshot ID.
	id int

	// e grouped by variety.
	entries map[istio_mixer_v1_template.TemplateVariety]*namespaceTable

	// identityAttribute defines which configuration scopes apply to a request.
	// default: target.service
	// The value of this attribute is expected to be a hostname of form "svc.$ns.suffix"
	identityAttribute string

	debugInfo *tableDebugInfo
}

// namespaceTable contains destination sets for a given template variety. It contains a mapping from namespaces
// to a flattened list of destinations. It also contains the defaultSet, which gets returned if no namespace-specific
// destination entry is found.
type namespaceTable struct {
	// destinations grouped by namespace
	entries map[string]*HandlerEntries

	// destinations for default namespace
	defaultSet *HandlerEntries
}

// HandlerEntries contains a list of destinations that should be targeted for a given namespace.
type HandlerEntries struct {
	entries []*HandlerEntry
}

// HandlerEntry contains a target handler, and a set of conditional instances.
type HandlerEntry struct {
	// ID of the entry. IDs are reused every time a table is recreated. Used for debugging.
	ID uint32

	// Handler to invoke
	Handler adapter.Handler

	// Template of the handler.
	Template *template.Info

	// Inputs that should be (conditionally) applied to the handler.
	Inputs []*InputSet
}

// InputSet is a set of instances that needs to be sent to a handler, based on the result of a condition.
type InputSet struct {
	// ID of the InputSet. IDs are reused every time a table is recreated. Used for debugging.
	ID uint32

	// Condition for applying this input set.
	Condition compiled.Expression

	// ResourceType is the resource type condition for this input set.
	ResourceType config.ResourceType

	// Builders for this input set for each instance that should be applied.
	Builders []template.InstanceBuilderFn

	// Mappers for attribute-generating adapters that map output attributes into the main attribute set.
	Mappers []template.OutputMapperFn
}

// Empty returns an empty routing table.
func Empty() *Table {
	return &Table{
		id:      -1,
		entries: make(map[istio_mixer_v1_template.TemplateVariety]*namespaceTable, 0),
	}
}

// ID of the routing table. Based on the config snapshot ID.
func (t *Table) ID() int {
	return t.id
}

// GetDestinations returns the set of destinations (handlers) for the given template variety and the attributes.
func (t *Table) GetDestinations(variety istio_mixer_v1_template.TemplateVariety, bag attribute.Bag) (*HandlerEntries, error) {
	destinations, ok := t.entries[variety]
	if !ok {
		if log.DebugEnabled() {
			log.Debugf("No destinations defined for variety. table(%d), variety(%d)", t.id, variety)
		}

		return emptySet, nil
	}

	destination, err := getDestination(bag, t.identityAttribute)
	if err != nil {
		log.Warnf("unable to determine destination set: '%v', table(%d), variety(%d)", err, t.id, variety)
		return nil, err
	}

	namespace := getNamespace(destination)
	destinationSet := destinations.entries[namespace]
	if destinationSet == nil {
		if log.DebugEnabled() {
			log.Debugf("using default destination set. table(%d), variety(%d), namespace(%s).", t.id, variety, namespace)
		}
		destinationSet = destinations.defaultSet
	}

	return destinationSet, nil
}

// Count returns the number of e in the set.
func (e *HandlerEntries) Count() int {
	return len(e.entries)
}

// Entries in the destination set.
func (e *HandlerEntries) Entries() []*HandlerEntry {
	return e.entries
}

// MaxInstances returns the maximum number of instances that can be built from this HandlerEntry.
func (e *HandlerEntry) MaxInstances() int {
	// TODO: Precalculate this
	c := 0
	for _, input := range e.Inputs {
		c += len(input.Builders)
	}

	return c
}

// ShouldApply returns true, if the instances from this input set should apply.
func (s *InputSet) ShouldApply(bag attribute.Bag) bool {
	if s.Condition == nil {
		return true
	}

	matches, err := s.Condition.EvaluateBoolean(bag)
	if err != nil {
		log.Warnf("input set condition evaluation error: InputSet(%d), error:'%v'", s.ID, err)
		return false
	}

	return matches
}
