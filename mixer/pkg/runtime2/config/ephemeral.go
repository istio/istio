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

// RulesKind defines the config kind name of mixer rules.
const RulesKind = "rule"

// AttributeManifestKind define the config kind name of attribute manifests.
const AttributeManifestKind = "attributemanifest"

// ContextProtocolTCP defines constant for tcp protocol.
const ContextProtocolTCP = "tcp"

const istioProtocol = "istio-protocol"

// Ephemeral configuration state that is being updated by incoming config change events.
type Ephemeral struct {
	// Static information
	adapters  map[string]*adapter.Info // maps adapter shortName to Info.
	templates map[string]template.Info

	// whether the attributes have changed since last snapshot
	attributesChanged bool

	// entries that are currently known.
	entries map[store.Key]*store.Resource

	// the latest snapshot.
	latest *Snapshot
}

func NewEphemeral(
	templates map[string]template.Info,
	adapters map[string]*adapter.Info,
	initialConfig map[store.Key]*store.Resource) *Ephemeral {

	return &Ephemeral{
		templates: templates,
		adapters:  adapters,

		attributesChanged: false,
		entries:           initialConfig,
		latest:            emptySnapshot,
	}
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

	return &Snapshot{
		attributes: &attributeFinder{attrs: attributes},
		handlers:   handlers,
		instances:  instances,
		rules:      rules,
	}
}

func (e *Ephemeral) processAttributeManifests() map[string]*configpb.AttributeManifest_AttributeInfo {
	if !e.attributesChanged && e.latest != nil {
		return e.latest.attributes.attrs
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
	// ATTRIBUTE_GENERATOR variety templates allows operators to write attributes
	// using the $out.<field name> convention, where $out refers to the output object from the attribute generating adapter.
	// The list of valid names for a given template is available in the template.Info.AttributeManifests object.
	for _, info := range e.templates {
		for _, v := range info.AttributeManifests {
			for an, at := range v.Attributes {
				attrs[an] = at
			}
		}
	}

	if log.DebugEnabled() {
		log.Debugf("%d known attributes", len(attrs))
	}

	return attrs
}

func (e *Ephemeral) processHandlerConfigs() map[string]*HandlerConfiguration {
	configs := make(map[string]*HandlerConfiguration)

	for key, resource := range e.entries {
		var info *adapter.Info
		var found bool
		if info, found = e.adapters[key.Kind]; !found {
			continue
		}

		config := &HandlerConfiguration{
			info:   info,
			name:   key.String(),
			params: resource.Spec,
		}

		configs[config.name] = config
	}

	if log.DebugEnabled() {
		log.Debugf("handler = %v", configs)
	}

	return configs
}

func (e *Ephemeral) processInstanceConfigs() map[string]*InstanceConfiguration {
	configs := make(map[string]*InstanceConfiguration)

	for key, resource := range e.entries {
		var info template.Info
		var found bool
		if info, found = e.templates[key.Kind]; !found {
			if log.DebugEnabled() {
				log.Debugf("Skipping configuration for unknown adapter: '%s/%s'", key.Namespace, key.Name)
			}
			continue
		}

		config := &InstanceConfiguration{
			template: info,
			name:     key.String(),
			params:   resource.Spec,
		}

		configs[config.name] = config
	}

	return configs
}

func (e *Ephemeral) processRuleConfigs(
	handlers map[string]*HandlerConfiguration,
	instances map[string]*InstanceConfiguration) []*RuleConfiguration {
	var configs []*RuleConfiguration

	for key, resource := range e.entries {
		if key.Kind != RulesKind {
			continue
		}

		cfg := resource.Spec.(*configpb.Rule)

		var actions []*ActionConfiguration
		for _, a := range cfg.Actions {
			handlerName := canonicalize(a.Handler, key.Namespace)
			handler, found := handlers[handlerName]
			if !found {
				log.Warnf("ConfigWarning unknown handler: %s", handlerName)
				continue

			}

			ruleInstances := []*InstanceConfiguration{}
			for _, instanceName := range a.Instances {
				instanceName := canonicalize(instanceName, key.Namespace)
				instance, found := instances[instanceName]
				if !found {
					log.Warnf("ConfigWarning unknown instance: %s", instanceName)
					continue
				}

				ruleInstances = append(ruleInstances, instance)
			}

			action := &ActionConfiguration{
				handler:   handler,
				instances: ruleInstances,
			}

			actions = append(actions, action)
		}

		config := &RuleConfiguration{
			namespace: key.Namespace,
			config:    cfg,
			actions:   actions,
		}
		// resourceType is used for backwards compatibility with labels: [istio-protocol: tcp]
		//rt := resourceType(resource.Metadata.Labels)

		configs = append(configs, config)
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
