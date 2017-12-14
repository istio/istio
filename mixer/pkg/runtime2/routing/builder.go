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
	table    *Table
	handlers *handler.Table
	expb     *compiled.ExpressionBuilder
}

func BuildTable(handlers *handler.Table, config *config.Snapshot, expb *compiled.ExpressionBuilder) *Table {
	b := &builder{
		table: &Table{
			id:      config.ID(),
			entries: make(map[istio_mixer_v1_template.TemplateVariety]*VarietyDestinations),
		},

		handlers: handlers,
		expb:     expb,
	}
	b.build(config)

	return b.table
}

func (b *builder) build(config *config.Snapshot) {
	for _, rule := range config.Rules() {

		for _, action := range rule.Actions() {

			handlerName := action.Handler().Name()
			handlerInstance := b.handlers.Get(handlerName)
			if handlerInstance == nil {
				// TODO: log the pruning of the action
				continue
			}

			for _, instance := range action.Instances() {
				// TODO: Should we single-instance instances? An instance config could be referenced multiple times.

				template := instance.Template()

				// TODO: Is this redundant?
				if !template.HandlerSupportsTemplate(handlerInstance) {
					// TODO: log the pruning of the instance
					continue
				}

				builder, err := template.CreateInstanceBuilder(instance.Params(), b.expb)
				if err != nil {
					// TODO: log the compile error
					continue
				}

				// TODO: Flatten destinations, so that we can match one rule condition to generate multiple instances,
				// for a given adapter.

				var condition compiled.Expression
				if rule.Match() != "" {
					condition, err = b.expb.Compile(rule.Match())
					if err != nil {
						// TODO: log
					}
					continue
				}

				b.add(rule.Namespace(), template, handlerInstance, condition, builder)
			}
		}
	}
}

func (b *builder) add(
	namespace string,
	t template.Info,
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
			entries: []Destination{},
		}
		byVariety.entries[namespace] = byNamespace
	}

	for _, d := range byNamespace.Entries() {
		if d.Handler == handler {
			// TODO: Flatten these, so that we can have multiple builders for a matching condition.
			input := &InputSet{
				Condition: condition,
				Builders:  []template.InstanceBuilder{builder},
			}
			d.Inputs = append(d.Inputs, input)
		}
	}
}
