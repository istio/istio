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
)

type builder struct {
	table                  *Table
	handlers               *handler.Table
	expb                   *compiled.ExpressionBuilder
	defaultConfigNamespace string
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
			entries:           make(map[istio_mixer_v1_template.TemplateVariety]*VarietyDestinations),
			identityAttribute: identityAttribute,
		},

		handlers: handlers,
		expb:     expb,
		defaultConfigNamespace: defaultConfigNamespace,
	}

	if debugInfo {
		b.table.debugInfo = &tableDebugInfo{
			instanceNames: make(map[template.InstanceBuilder]string),
			handlerNames: make(map[adapter.Handler]string),
			matchConditions: make(map[compiled.Expression]string),
		}
	}

	b.build(config)

	return b.table
}

func (b *builder) build(config *config.Snapshot) {
	for _, rule := range config.Rules {

		var condition compiled.Expression
		var err error
		if rule.Match != "" {
			condition, err = b.expb.Compile(rule.Match)
			if err != nil {
				// TODO: log
			}
		}

		for _, action := range rule.Actions {
			handlerName := action.Handler.Name
			handlerInstance, found := b.handlers.GetHealthyHandler(handlerName)
			if !found {
				// TODO: log the pruning of the action
				continue
			}
			if b.table.debugInfo != nil {
				b.table.debugInfo.handlerNames[handlerInstance] = handlerName
			}

			for _, instance := range action.Instances {
				// TODO: Should we single-instance instances? An instance config could be referenced multiple times.

				template := instance.Template

				// TODO: Is this redundant?
				if !template.HandlerSupportsTemplate(handlerInstance) {
					// TODO: log the pruning of the instance
					continue
				}

				builder, err := template.CreateInstanceBuilder(instance.Params, b.expb)
				if err != nil {
					// TODO: log the compile error
					continue
				}

				// TODO: Flatten destinations, so that we can match one rule condition to generate multiple instances,
				// for a given adapter.

				if b.table.debugInfo != nil {
					b.table.debugInfo.instanceNames[builder] = instance.Name
					if condition != nil {
						b.table.debugInfo.matchConditions[condition] = rule.Match
					}
				}

				b.add(rule.Namespace, template, handlerInstance, condition, builder)
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
	builder template.InstanceBuilder) {

	byVariety, found := b.table.entries[t.Variety]
	if !found {
		byVariety = &VarietyDestinations{
			entries: make(map[string]*DestinationSet),
		}
		b.table.entries[t.Variety] = byVariety
	}

	byNamespace, found := byVariety.entries[namespace]
	if !found {
		byNamespace = &DestinationSet{
			entries: []*Destination{},
		}
		byVariety.entries[namespace] = byNamespace
	}

	for _, d := range byNamespace.Entries() {
		// Try to flatting destinations.
		if d.Handler == handler {
			// Try to flatting input sets.
			for _, existing := range d.Inputs {
				if existing.Condition == condition {
					existing.Builders = append(existing.Builders, builder)
					return
				}
			}

			input := &InputSet{
				Condition: condition,
				Builders:  []template.InstanceBuilder{builder},
			}
			d.Inputs = append(d.Inputs, input)
			return
		}
	}

	input := &InputSet{
		Condition: condition,
		Builders:  []template.InstanceBuilder{builder},
	}
	destination := &Destination{
		Handler: handler,
		Template: t,
		Inputs: []*InputSet{input},
	}
	byNamespace.entries = append(byNamespace.entries, destination)
}
