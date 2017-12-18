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
	"istio.io/istio/mixer/pkg/log"
	"istio.io/istio/mixer/pkg/runtime2/config"
	"istio.io/istio/mixer/pkg/runtime2/handler"
	"istio.io/istio/mixer/pkg/template"
)

type builder struct {
	table                  *Table
	handlers               *handler.Table
	expb                   *compiled.ExpressionBuilder
	defaultConfigNamespace string
	nextIdCounter          uint32
}

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
		nextIdCounter:          1,
	}

	if debugInfo {
		b.table.debugInfo = &tableDebugInfo{
			handlerEntries: make(map[uint32]string),
			inputSets:      make(map[uint32]inputSetDebugInfo),
		}
	}

	b.build(config)

	return b.table
}

func (b *builder) nextID() uint32 {
	id := b.nextIdCounter
	b.nextIdCounter++
	return id
}

func (b *builder) build(config *config.Snapshot) {
	// Go over the rules and find the right slot in the table to put entries in.
	for _, rule := range config.Rules {

		var condition compiled.Expression
		var err error
		if rule.Match != "" {
			condition, err = b.expb.Compile(rule.Match)
			if err != nil {
				log.Warnf("Unable to compile match condition expression: '%v', rule: '%s', expression: '%s'",
					err, rule.Name, rule.Match)
				continue
			}
		}

		for i, action := range rule.Actions {
			handlerName := action.Handler.Name
			handlerInstance, found := b.handlers.GetHealthyHandler(handlerName)
			if !found {
				log.Warnf("Unable to find a handler for action. rule[action]: '%s[%d]', handler: '%s'",
					rule.Name, i, handlerName)
				continue
			}

			for _, instance := range action.Instances {
				// TODO: Should we single-instance instances? An instance config could be referenced multiple times.

				t := instance.Template

				// TODO: Is this redundant?
				if !t.HandlerSupportsTemplate(handlerInstance) {
					log.Warnf("The handler doesn't support the template: handler:'%s', template:'%s'",
						handlerName, t.Name)
					continue
				}

				builder := t.CreateInstanceBuilder(instance.Name, instance.Params, b.expb)

				var mapper template.OutputMapperFn
				if t.Variety == istio_mixer_v1_template.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR {
					mapper = t.CreateOutputMapperFn(instance.Params, b.expb)
				}

				b.add(rule.Namespace, t, handlerInstance, condition, builder, mapper, handlerName, instance.Name, rule.Match)
			}
		}
	}

	// Prefix all namespace destinations with the destinations from the default namespace.
	for _, vDestinations := range b.table.entries {
		defaultSet, found := vDestinations.entries[b.defaultConfigNamespace]
		if !found {
			// Nothing to do here
			// TODO: log
			continue
		}

		for namespace, set := range vDestinations.entries {
			if namespace == b.defaultConfigNamespace {
				// Skip the default namespace itself
				continue
			}

			set.entries = append(defaultSet.entries, set.entries...)
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
	matchText string) {

	// Find or create variety entry.
	byVariety, found := b.table.entries[t.Variety]
	if !found {
		byVariety = &namespaceTable{
			entries: make(map[string]*handlerEntries),
		}
		b.table.entries[t.Variety] = byVariety
	}

	// Find or create the namespace entry.
	byNamespace, found := byVariety.entries[namespace]
	if !found {
		byNamespace = &handlerEntries{
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

		if b.table.debugInfo != nil {
			b.table.debugInfo.handlerEntries[byHandler.ID] = handlerName
		}
	}

	// Find or create the input set.
	var inputSet *InputSet
	for _, set := range byHandler.Inputs {
		// TODO: This doesn't flatten accross all conditions, only for actions coming from the same rule. We can
		// single instance based on the expression text as well.
		if set.Condition == condition {
			inputSet = set
			break
		}
	}

	if inputSet == nil {
		inputSet = &InputSet{
			ID:        b.nextID(),
			Condition: condition,
			Builders:  []template.InstanceBuilderFn{},
			Mappers:   []template.OutputMapperFn{},
		}
		byHandler.Inputs = append(byHandler.Inputs, inputSet)

		if b.table.debugInfo != nil {
			b.table.debugInfo.inputSets[inputSet.ID] = inputSetDebugInfo{match: matchText, instanceNames: []string{}}
		}
	}

	// Append the builder & mapper.
	inputSet.Builders = append(inputSet.Builders, builder)

	// TODO: mapper should either be nil for all entries, or be present for all builders. We should validate.
	if mapper != nil {
		inputSet.Mappers = append(inputSet.Mappers, mapper)
	}

	if b.table.debugInfo != nil {
		debugInfo := b.table.debugInfo.inputSets[inputSet.ID]
		debugInfo.instanceNames = append(debugInfo.instanceNames, instanceName)
		b.table.debugInfo.inputSets[inputSet.ID] = debugInfo
	}
}
