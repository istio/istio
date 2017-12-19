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
	"istio.io/istio/mixer/pkg/il/compiled"
	"istio.io/istio/mixer/pkg/runtime2/config"
	"istio.io/istio/mixer/pkg/runtime2/handler"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/istio/pkg/log"
)

type builder struct {
	// table that is being built.
	table                  *Table
	handlers               *handler.Table
	expb                   *compiled.ExpressionBuilder
	defaultConfigNamespace string
	nextIDCounter          uint32

	// Ephemeral data, that can also be used as debugging info.

	// The handler names by the handler entry id.
	handlerNamesByID map[uint32]string

	// match condition sets by the input set id.
	matchesByID map[uint32]string

	// instanceName set of builders by the input set.
	instanceNamesByID map[uint32][]string
}

// BuildTable builds and returns a routing table. If debugInfo is set, the returned table will have debugging information
// attached, which will show up in String() call.
func BuildTable(
	handlers *handler.Table,
	config *config.Snapshot,
	expb *compiled.ExpressionBuilder,
	identityAttribute string,
	defaultConfigNamespace string,
	debugInfo bool) *Table {

	b := &builder{

		table: &Table{
			id:                config.ID,
			entries:           make(map[istio_mixer_v1_template.TemplateVariety]*namespaceTable),
			identityAttribute: identityAttribute,
		},

		handlers: handlers,
		expb:     expb,
		defaultConfigNamespace: defaultConfigNamespace,
		nextIDCounter:          1,

		handlerNamesByID:  make(map[uint32]string),
		matchesByID:       make(map[uint32]string),
		instanceNamesByID: make(map[uint32][]string),
	}

	b.build(config)

	if debugInfo {
		b.table.debugInfo = &tableDebugInfo{
			handlerNamesByID:  b.handlerNamesByID,
			matchesByID:       b.matchesByID,
			instanceNamesByID: b.instanceNamesByID,
		}
	}
	return b.table
}

func (b *builder) nextID() uint32 {
	id := b.nextIDCounter
	b.nextIDCounter++
	return id
}

func (b *builder) build(config *config.Snapshot) {
	// Go over the rules and find the right slot in the table to put e in.
	for _, rule := range config.Rules {

		var condition compiled.Expression
		var err error
		if rule.Match != "" {
			condition, err = b.expb.Compile(rule.Match)
			if err != nil {
				log.Warnf("Unable to compile match condition expression: '%v', rule='%s', expression='%s'",
					err, rule.Name, rule.Match)
				continue
			}
		}

		for i, action := range rule.Actions {
			handlerName := action.Handler.Name
			handlerInstance, found := b.handlers.GetHealthyHandler(handlerName)
			if !found {
				log.Warnf("Unable to find a handler for action. rule[action]='%s[%d]', handler='%s'",
					rule.Name, i, handlerName)
				continue
			}

			for _, instance := range action.Instances {
				// TODO: Should we single-instance instances? An instance config could be referenced multiple times.

				t := instance.Template

				// TODO: Is this redundant?
				if !t.HandlerSupportsTemplate(handlerInstance) {
					log.Warnf("The handler doesn't support the template: handler='%s', template='%s'",
						handlerName, t.Name)
					continue
				}

				builder := t.CreateInstanceBuilder(instance.Name, instance.Params, b.expb)

				var mapper template.OutputMapperFn
				if t.Variety == istio_mixer_v1_template.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR {
					mapper = t.CreateOutputMapperFn(instance.Params, b.expb)
				}

				b.add(rule.Namespace, t, handlerInstance, condition, builder, mapper,
					handlerName, instance.Name, rule.Match, rule.ResourceType)
			}
		}
	}

	// Capture the default namespace rule set and flatten all default namespace rule into other namespace tables for
	// faster processing.
	for _, vDestinations := range b.table.entries {
		defaultSet, found := vDestinations.entries[b.defaultConfigNamespace]
		if !found {
			log.Warnf("No destination sets found for the default namespace '%s'.", b.defaultConfigNamespace)
			defaultSet = emptySet
		}
		// Set the default rule set for the variety.
		vDestinations.defaultSet = defaultSet

		if defaultSet.Count() != 0 {
			// Prefix all namespace destinations with the destinations from the default namespace.
			for namespace, set := range vDestinations.entries {
				if namespace == b.defaultConfigNamespace {
					// Skip the default namespace itself
					continue
				}

				set.entries = append(defaultSet.entries, set.entries...)
			}
		}
	}

	// walk through and sortByHandlerName the entries for stable ordering.
	for _, vDestinations := range b.table.entries {
		for _, namespaces := range vDestinations.entries {
			namespaces.sortByHandlerName(b.handlerNamesByID)

			for _, entry := range namespaces.entries {
				entry.sortByMatchText(b.matchesByID)

				for _, i := range entry.Inputs {
					i.sortByInstanceName(b.instanceNamesByID[i.ID])
				}
			}
		}
	}
}

func (b *builder) add(
	namespace string,
	t *template.Info,
	handler adapter.Handler,
	condition compiled.Expression,
	builder template.InstanceBuilderFn,
	mapper template.OutputMapperFn,
	handlerName string,
	instanceName string,
	matchText string,
	resourceType config.ResourceType) {

	// Find or create variety entry.
	byVariety, found := b.table.entries[t.Variety]
	if !found {
		byVariety = &namespaceTable{
			entries: make(map[string]*HandlerEntries),
		}
		b.table.entries[t.Variety] = byVariety
	}

	// Find or create the namespace entry.
	byNamespace, found := byVariety.entries[namespace]
	if !found {
		byNamespace = &HandlerEntries{
			entries: []*HandlerEntry{},
		}
		byVariety.entries[namespace] = byNamespace
	}

	// Find or create the handler entry.
	var byHandler *HandlerEntry
	for _, d := range byNamespace.Entries() {
		if d.Handler == handler {
			byHandler = d
			break
		}
	}

	if byHandler == nil {
		byHandler = &HandlerEntry{
			ID:       b.nextID(),
			Handler:  handler,
			Template: t,
			Inputs:   []*InputSet{},
		}
		byNamespace.entries = append(byNamespace.entries, byHandler)

		b.handlerNamesByID[byHandler.ID] = handlerName
	}

	// Find or create the input set.
	var inputSet *InputSet
	for _, set := range byHandler.Inputs {
		// TODO: This doesn't flatten across all actions, but only for actions coming from the same rule. We can
		// single instance based on the expression text as well.
		if set.Condition == condition && set.ResourceType == resourceType {
			inputSet = set
			break
		}
	}

	if inputSet == nil {
		inputSet = &InputSet{
			ID:           b.nextID(),
			Condition:    condition,
			ResourceType: resourceType,
			Builders:     []template.InstanceBuilderFn{},
			Mappers:      []template.OutputMapperFn{},
		}
		byHandler.Inputs = append(byHandler.Inputs, inputSet)

		if matchText != "" {
			b.matchesByID[inputSet.ID] = matchText
		}

		instanceNames, found := b.instanceNamesByID[inputSet.ID]
		if !found {
			instanceNames = make([]string, 0, 1)
		}
		b.instanceNamesByID[inputSet.ID] = instanceNames
	}

	// Append the builder & mapper.
	inputSet.Builders = append(inputSet.Builders, builder)

	// Recalculate the maximum number of instances that can be created
	byHandler.recalculateMaxInstances()

	// TODO: mapper should either be nil for all e, or be present for all builders. We should validate.
	if mapper != nil {
		inputSet.Mappers = append(inputSet.Mappers, mapper)
	}

	instanceNames := b.instanceNamesByID[inputSet.ID]
	instanceNames = append(instanceNames, instanceName)
	b.instanceNamesByID[inputSet.ID] = instanceNames
}
