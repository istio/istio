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

package config

import (
	"istio.io/istio/mixer/pkg/adapter"
	configpb "istio.io/istio/mixer/pkg/config/proto"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/log"
	"istio.io/istio/mixer/pkg/template"
)

// RulesKind defines the config kind Name of mixer Rules.
const RulesKind = "rule"

// AttributeManifestKind define the config kind Name of attribute manifests.
const AttributeManifestKind = "attributemanifest"

// ContextProtocolTCP defines constant for tcp protocol.
const ContextProtocolTCP = "tcp"

const istioProtocol = "istio-protocol"

// Ephemeral configuration state that gets updated by incoming config change events.
type Ephemeral struct {
	// Static information
	adapters  map[string]*adapter.Info // maps adapter shortName to Info.
	templates map[string]*template.Info

	// next snapshot id
	nextId int

	// whether the Attributes have changed since last snapshot
	attributesChanged bool

	// entries that are currently known.
	entries map[store.Key]*store.Resource

	// the latest snapshot.
	latest *Snapshot
}

func NewEphemeral(
	templates map[string]*template.Info,
	adapters map[string]*adapter.Info) *Ephemeral {

	e := &Ephemeral{
		templates: templates,
		adapters:  adapters,

		nextId: 0,

		attributesChanged: false,
		entries:           make(map[store.Key]*store.Resource, 0),
		latest:            nil,
	}

	// build the initial snapshot.
	_ = e.BuildSnapshot()

	return e
}

func (e *Ephemeral) SetState(state map[store.Key]*store.Resource) {
	e.entries = state
}

func (e *Ephemeral) ApplyEvents(events []*store.Event) {
	for _, ev := range events {

		if ev.Kind == AttributeManifestKind {
			e.attributesChanged = true
		}

		switch ev.Type {
		case store.Update:
			e.entries[ev.Key] = ev.Value
		case store.Delete:
			delete(e.entries, ev.Key)
		}
	}
}

func (e *Ephemeral) BuildSnapshot() *Snapshot {
	attributes := e.processAttributeManifests()

	handlers := e.processHandlerConfigs()

	instances := e.processInstanceConfigs()

	rules := e.processRuleConfigs(handlers, instances)

	e.attributesChanged = false

	id := e.nextId
	e.nextId++

	s := &Snapshot{
		ID:         id,
		Templates:  e.templates,
		Adapters:   e.adapters,
		Attributes: &attributeFinder{attrs: attributes},
		Handlers:   handlers,
		Instances:  instances,
		Rules:      rules,
	}

	e.latest = s

	return s
}

func (e *Ephemeral) processAttributeManifests() map[string]*configpb.AttributeManifest_AttributeInfo {
	if !e.attributesChanged && e.latest != nil {
		return e.latest.Attributes.attrs
	}

	attrs := make(map[string]*configpb.AttributeManifest_AttributeInfo)
	for k, obj := range e.entries {
		if k.Kind != AttributeManifestKind {
			continue
		}
		cfg := obj.Spec
		for an, at := range cfg.(*configpb.AttributeManifest).Attributes {
			attrs[an] = at
		}
	}

	// append all the well known attribute vocabulary from the templates.
	//
	// ATTRIBUTE_GENERATOR variety templates allows operators to write Attributes
	// using the $out.<field Name> convention, where $out refers to the output object from the attribute generating adapter.
	// The list of valid names for a given Template is available in the Template.Info.AttributeManifests object.
	for _, info := range e.templates {
		for _, v := range info.AttributeManifests {
			for an, at := range v.Attributes {
				attrs[an] = at
			}
		}
	}

	log.Debugf("%d known Attributes", len(attrs))

	return attrs
}

func (e *Ephemeral) processHandlerConfigs() map[string]*Handler {
	configs := make(map[string]*Handler)

	for key, resource := range e.entries {
		var info *adapter.Info
		var found bool
		if info, found = e.adapters[key.Kind]; !found {
			continue
		}

		config := &Handler{
			Name:    key.String(),
			Adapter: info,
			Params:  resource.Spec,
		}

		configs[config.Name] = config
	}

	log.Debugf("Handler = %v", configs)

	return configs
}

func (e *Ephemeral) processInstanceConfigs() map[string]*Instance {
	configs := make(map[string]*Instance)

	for key, resource := range e.entries {
		var info *template.Info
		var found bool
		if info, found = e.templates[key.Kind]; !found {
			continue
		}

		config := &Instance{
			Name:     key.String(),
			Template: info,
			Params:   resource.Spec,
		}

		configs[config.Name] = config
	}

	return configs
}

func (e *Ephemeral) processRuleConfigs(
	handlers map[string]*Handler,
	instances map[string]*Instance) []*Rule {

	var configs []*Rule

	for ruleKey, resource := range e.entries {
		if ruleKey.Kind != RulesKind {
			continue
		}

		cfg := resource.Spec.(*configpb.Rule)

		var actions []*Action
		for i, a := range cfg.Actions {

			handlerName := canonicalize(a.Handler, ruleKey.Namespace)
			handler, found := handlers[handlerName]
			if !found {
				log.Warnf("ConfigWarning unknown Handler: %s", handlerName)
				continue

			}

			actionInstances := []*Instance{}
			for _, instanceName := range a.Instances {
				instanceName := canonicalize(instanceName, ruleKey.Namespace)
				instance, found := instances[instanceName]
				if !found {
					log.Warnf("ConfigWarning unknown instance: %s", instanceName)
					continue
				}

				actionInstances = append(actionInstances, instance)
			}

			if len(actionInstances) == 0 {
				log.Warnf("ConfigWarning no valid instances found in action: %s[%d]", ruleKey.String(), i)
				continue
			}

			action := &Action{
				Handler:   handler,
				Instances: actionInstances,
			}

			actions = append(actions, action)
		}

		if len(actions) == 0 {
			log.Warnf("ConfigWarning no valid actions found in rule: %s", ruleKey.String())
			continue
		}

		rule := &Rule{
			Name:      ruleKey.String(),
			Namespace: ruleKey.Namespace,
			Actions:   actions,
		}

		// resourceType is used for backwards compatibility with labels: [istio-protocol: tcp]
		//rt := resourceType(resource.Metadata.Labels)

		configs = append(configs, rule)
	}

	return configs
}

// resourceType maps labels to rule types.
func resourceType(labels map[string]string) ResourceType {
	rt := defaultResourcetype()
	if ContextProtocolTCP == labels[istioProtocol] {
		rt.protocol = protocolTCP
	}
	return rt
}
