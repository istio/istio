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

// Table is the main routing table. It is used to find the set of handlers that should be invoked, along with the
// instance builders and match conditions.
type Table struct {

	// ID of this table. This is based on the config snapshot ID. IDs are unique within the life-span of a Mixer instance.
	ID int64

	// namespaceTables grouped by variety.
	entries map[istio_mixer_v1_template.TemplateVariety]*varietyTable

	debugInfo *tableDebugInfo
}

// varietyTable contains destination sets for a given template variety. It contains a mapping from namespaces
// to a flattened list of destinations. It also contains the defaultSet, which gets returned if no namespace-specific
// destination entry is found.
type varietyTable struct {
	// destinations grouped by namespace. These contain destinations from the default namespace as well.
	entries map[string]*NamespaceTable

	// destinations for default namespace
	defaultSet *NamespaceTable
}

// NamespaceTable contains a list of destinations that should be targeted for a given namespace.
type NamespaceTable struct {
	entries []*Destination
}

var emptyDestinations = &NamespaceTable{
	entries: []*Destination{},
}

// Destination contains a target handler, and instances to send, grouped by the conditional match that applies to them.
type Destination struct {
	// ID of the entry. IDs are reused every time a table is recreated. Used for debugging.
	ID uint32

	// Handler to invoke
	Handler adapter.Handler

	// HandlerName is the name of the handler. Used for monitoring/logging purposes.
	HandlerName string

	// AdapterName is the name of the adapter. Used for monitoring/logging purposes.
	AdapterName string

	// Template of the handler.
	Template *template.Info

	// InputGroup that should be (conditionally) applied to the handler.
	InputGroups []*InputGroup

	// Maximum number of instances that can be created from this entry.
	maxInstances int

	// FriendlyName is the friendly name of this configured handler entry. Used for monitoring/logging purposes.
	FriendlyName string

	// Perf counters for keeping track of dispatches to adapters/handlers.
	Counters DestinationCounters
}

// InputGroup is a set of instances that needs to be sent to a handler, grouped by a condition expression.
type InputGroup struct {
	// ID of the InputGroup. IDs are reused every time a table is recreated. Used for debugging.
	ID uint32

	// Condition for applying this input group.
	Condition compiled.Expression

	// TODO: This should be removed when https://github.com/istio/istio/issues/2139 is fixed.
	// ResourceType is the resource type condition for this input group.
	ResourceType config.ResourceType

	// Builders for the instances in this input set for each instance that should be applied.
	Builders []template.InstanceBuilderFn

	// Mappers for attribute-generating adapters that map output attributes into the main attribute set.
	Mappers []template.OutputMapperFn
}

var emptyTable = &Table{ID: -1}

// Empty returns an empty routing table.
func Empty() *Table {
	return emptyTable
}

// GetDestinations returns the set of destinations (handlers) for the given template variety and for the given namespace.
func (t *Table) GetDestinations(variety istio_mixer_v1_template.TemplateVariety, namespace string) *NamespaceTable {
	destinations, ok := t.entries[variety]
	if !ok {
		log.Debugf("No destinations found for variety: table='%d', variety='%d'", t.ID, variety)

		return emptyDestinations
	}

	destinationSet := destinations.entries[namespace]
	if destinationSet == nil {
		log.Debugf("no rules for namespace, using defaults: table='%d', variety='%d', ns='%s'", t.ID, variety, namespace)
		destinationSet = destinations.defaultSet
	}

	return destinationSet
}

// Count returns the number of entries contained.
func (d *NamespaceTable) Count() int {
	return len(d.entries)
}

// Entries in the table.
func (d *NamespaceTable) Entries() []*Destination {
	return d.entries
}

// MaxInstances returns the maximum number of instances that can be built from this Destination.
func (d *Destination) MaxInstances() int {
	return d.maxInstances
}

// used during building to recalculate maxInstances, after a modification.
func (d *Destination) recalculateMaxInstances() {
	c := 0
	for _, input := range d.InputGroups {
		c += len(input.Builders)
	}

	d.maxInstances = c
}

// Matches returns true, if the instances from this input set should be used for the given attribute bag.
func (i *InputGroup) Matches(bag attribute.Bag) bool {
	if i.Condition == nil {
		return true
	}

	matches, err := i.Condition.EvaluateBoolean(bag)
	if err != nil {
		log.Warnf("input set condition evaluation error: id='%d', error='%v'", i.ID, err)
		return false
	}

	return matches
}
