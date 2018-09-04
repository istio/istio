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
	tpb "istio.io/api/mixer/adapter/model/v1beta1"
	descriptor "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/lang/compiled"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/istio/pkg/log"
)

// Table is the main routing table. It is used to find the set of handlers that should be invoked, along with the
// instance builders and match conditions.
type Table struct {

	// id of this table. This is based on the config snapshot id. IDs are unique within the life-span of a Mixer instance.
	id int64

	// namespaceTables grouped by variety.
	entries map[tpb.TemplateVariety]*varietyTable

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

// NamespaceTable contains a list of destinations and operations that should be targeted for a given namespace.
type NamespaceTable struct {
	entries    []*Destination
	operations []*OperationGroup
}

var emptyDestinations = &NamespaceTable{}

// OperationGroup is a group of operations, conditioned by an expression
type OperationGroup struct {
	// Condition is the guard for the operations
	Condition compiled.Expression
	// Operations is a list of rule operations
	Operations []*HeaderOperation
}

// Destination contains a target handler, and instances to send, grouped by the conditional match that applies to them.
type Destination struct {
	// id of the entry. IDs are reused every time a table is recreated. Used for debugging.
	id uint32

	// Handler to invoke
	Handler adapter.Handler

	// HandlerName is the name of the handler. Used for monitoring/logging purposes.
	HandlerName string

	// AdapterName is the name of the adapter. Used for monitoring/logging purposes.
	AdapterName string

	// Template of the handler.
	Template *TemplateInfo

	// InstanceGroups that should be (conditionally) applied to the handler.
	InstanceGroups []*InstanceGroup

	// Maximum number of instances that can be created from this entry.
	maxInstances int

	// FriendlyName is the friendly name of this configured handler entry. Used for monitoring/logging purposes.
	FriendlyName string
}

// NamedBuilder holds a builder function and the short name of the associated instance.
type NamedBuilder struct {
	InstanceShortName string
	Builder           template.InstanceBuilderFn

	// ActionName is the name of the rule action. Used for computing rule operations from template output.
	ActionName string

	// EvaluateFn is the reverse evaluate function from the output
	EvaluateFn template.EvaluateOutputFn
}

// TemplateInfo is the common data that is needed from a template
type TemplateInfo struct {
	Name                string
	Variety             tpb.TemplateVariety
	DispatchReport      template.DispatchReportFn
	DispatchCheck       template.DispatchCheckFn
	DispatchCheckOutput template.DispatchCheckOutputFn
	DispatchQuota       template.DispatchQuotaFn
	DispatchGenAttrs    template.DispatchGenerateAttributesFn
	EvaluateOutput      template.EvaluateOutputFn
}

func buildTemplateInfo(info *template.Info) *TemplateInfo {
	return &TemplateInfo{
		Name:                info.Name,
		Variety:             info.Variety,
		DispatchReport:      info.DispatchReport,
		DispatchCheck:       info.DispatchCheck,
		DispatchCheckOutput: info.DispatchCheckOutput,
		DispatchQuota:       info.DispatchQuota,
		DispatchGenAttrs:    info.DispatchGenAttrs,
		EvaluateOutput:      info.EvaluateOutput,
	}
}

// InstanceGroup is a set of instances that needs to be sent to a handler, grouped by a condition expression.
type InstanceGroup struct {
	// id of the InstanceGroup. IDs are reused every time a table is recreated. Used for debugging.
	id uint32

	// Condition for applying this instance group.
	Condition compiled.Expression

	// Builders for the instances in this group for each instance that should be applied.
	Builders []NamedBuilder

	// Mappers for attribute-generating adapters that map output attributes into the main attribute set.
	Mappers []template.OutputMapperFn
}

// OperationType enumeration for the route directive header operation template.
type OperationType int

const (
	// RequestHeaderOperation is an operation on the request headers
	RequestHeaderOperation OperationType = iota
	// ResponseHeaderOperation is an operation on the response headers
	ResponseHeaderOperation
)

// HeaderOperation is an intermediate form of a rule header operation.
type HeaderOperation struct {
	Type      OperationType
	Name      compiled.Expression
	Value     compiled.Expression
	Operation descriptor.Rule_HeaderOperationTemplate_Operation
}

var emptyTable = &Table{id: -1}

// Empty returns an empty routing table.
func Empty() *Table {
	return emptyTable
}

// ID of the table. Based on the Config Snapshot id.
func (t *Table) ID() int64 {
	return t.id
}

// GetDestinations returns the set of destinations (handlers) for the given template variety in the given namespace.
// Template variety CHECK and CHECK_WITH_OUTPUT are considered equivalent, and will produce the same destinations, which
// is the union of the two.
func (t *Table) GetDestinations(variety tpb.TemplateVariety, namespace string) []*Destination {
	switch variety {
	case tpb.TEMPLATE_VARIETY_CHECK, tpb.TEMPLATE_VARIETY_CHECK_WITH_OUTPUT:
		checkDestinations := t.getVarietyDestinations(tpb.TEMPLATE_VARIETY_CHECK, namespace)
		checkOutputDestinations := t.getVarietyDestinations(tpb.TEMPLATE_VARIETY_CHECK_WITH_OUTPUT, namespace)
		return append(checkDestinations, checkOutputDestinations...)
	default:
		return t.getVarietyDestinations(variety, namespace)
	}
}

// GetOperations returns the set of rule operations for the given template variety in the given namespace.
func (t *Table) GetOperations(namespace string) []*OperationGroup {
	varietyEntry, ok := t.entries[tpb.TEMPLATE_VARIETY_CHECK_WITH_OUTPUT]
	if !ok {
		return nil
	}

	out := varietyEntry.entries[namespace]
	if out == nil {
		out = varietyEntry.defaultSet
	}

	return out.operations
}

func (t *Table) getVarietyDestinations(variety tpb.TemplateVariety, namespace string) []*Destination {
	destinations, ok := t.entries[variety]
	if !ok {
		log.Debugf("No destinations found for variety: table='%d', variety='%d'", t.id, variety)

		return nil
	}

	destinationSet := destinations.entries[namespace]
	if destinationSet == nil {
		log.Debugf("no rules for namespace, using defaults: table='%d', variety='%d', ns='%s'", t.id, variety, namespace)
		destinationSet = destinations.defaultSet
	}

	return destinationSet.Entries()
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
	for _, input := range d.InstanceGroups {
		c += len(input.Builders)
	}

	d.maxInstances = c
}

// Matches returns true, if the instances from this input set should be used for the given attribute bag.
func Matches(condition compiled.Expression, bag attribute.Bag) bool {
	if condition == nil {
		return true
	}

	matches, err := condition.EvaluateBoolean(bag)
	if err != nil {
		log.Warnf("input set condition evaluation error:'%v'", err)
		return false
	}

	return matches
}
