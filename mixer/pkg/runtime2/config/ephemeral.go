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

package config

import (
	"istio.io/api/mixer/v1/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/expr"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/istio/pkg/log"
)

// Ephemeral configuration state that gets updated by incoming config change events. By itself, the data contained
// is not meaningful. BuildSnapshot must be called to create a new snapshot instance, which contains fully resolved
// config.
type Ephemeral struct {
	// Static information
	adapters  map[string]*adapter.Info
	templates map[string]*template.Info

	// next snapshot id
	nextID int64

	// entries that are currently known.
	entries map[store.Key]*store.Resource

	// attributes from the last config state update. If the manifest hasn't changed since the last config update
	// the attributes are reused.
	cachedAttributes map[string]*config.AttributeManifest_AttributeInfo
}

// NewEphemeral returns a new Ephemeral instance.
func NewEphemeral(
	templates map[string]*template.Info,
	adapters map[string]*adapter.Info) *Ephemeral {

	e := &Ephemeral{
		templates: templates,
		adapters:  adapters,

		nextID: 0,

		entries: make(map[store.Key]*store.Resource),

		cachedAttributes: nil,
	}

	// build the initial snapshot.
	_ = e.BuildSnapshot()

	return e
}

// SetState with the supplied state map. All existing ephemeral state is overwritten.
func (e *Ephemeral) SetState(state map[store.Key]*store.Resource) {
	e.entries = state

	for k := range state {
		if k.Kind == AttributeManifestKind {
			e.cachedAttributes = nil
			break
		}
	}
}

// ApplyEvent to the internal ephemeral state. This gets called by an external event listener to relay store change
// events to this ephemeral config object.
func (e *Ephemeral) ApplyEvent(event *store.Event) {

	if event.Kind == AttributeManifestKind {
		e.cachedAttributes = nil
		log.Debug("Received attribute manifest change event.")
	}

	switch event.Type {
	case store.Update:
		e.entries[event.Key] = event.Value
	case store.Delete:
		delete(e.entries, event.Key)
	}
}

// BuildSnapshot builds a stable, fully-resolved snapshot view of the configuration.
func (e *Ephemeral) BuildSnapshot() *Snapshot {
	id := e.nextID
	e.nextID++

	log.Debugf("Building new config.Snapshot: id='%d'", id)

	// Allocate new counters, to use with the new snapshot.
	counters := newCounters(id)

	attributes := e.processAttributeManifests(counters)

	handlers := e.processHandlerConfigs(counters)

	instances := e.processInstanceConfigs(counters)

	rules := e.processRuleConfigs(handlers, instances, counters)

	s := &Snapshot{
		ID:         id,
		Templates:  e.templates,
		Adapters:   e.adapters,
		Attributes: expr.NewFinder(attributes),
		Handlers:   handlers,
		Instances:  instances,
		Rules:      rules,
		Counters:   counters,
	}

	e.cachedAttributes = attributes

	log.Infof("Built new config.Snapshot: id='%d'", id)
	log.Debugf("config.Snapshot contents:\n%s", s)
	return s
}

func (e *Ephemeral) processAttributeManifests(counters Counters) map[string]*config.AttributeManifest_AttributeInfo {
	if e.cachedAttributes != nil {
		return e.cachedAttributes
	}

	attrs := make(map[string]*config.AttributeManifest_AttributeInfo)
	for k, obj := range e.entries {
		if k.Kind != AttributeManifestKind {
			continue
		}

		log.Debug("Start processing attributes from changed manifest...")

		cfg := obj.Spec
		for an, at := range cfg.(*config.AttributeManifest).Attributes {
			attrs[an] = at

			log.Debugf("Attribute '%s': '%s'.", an, at.ValueType)
		}
	}

	// append all the well known attribute vocabulary from the templates.
	//
	// ATTRIBUTE_GENERATOR variety templates allows operators to write Attributes
	// using the $out.<field Name> convention, where $out refers to the output object from the attribute generating adapter.
	// The list of valid names for a given Template is available in the Template.Info.AttributeManifests object.
	for _, info := range e.templates {
		log.Debugf("Processing attributes from template: '%s'", info.Name)

		for _, v := range info.AttributeManifests {
			for an, at := range v.Attributes {
				attrs[an] = at

				log.Debugf("Attribute '%s': '%s'", an, at.ValueType)
			}
		}
	}

	log.Debug("Completed processing attributes.")
	counters.attributes.Add(float64(len(attrs)))

	return attrs
}

func (e *Ephemeral) processHandlerConfigs(counters Counters) map[string]*Handler {
	handlers := make(map[string]*Handler, len(e.adapters))

	for key, resource := range e.entries {
		var info *adapter.Info
		var found bool
		if info, found = e.adapters[key.Kind]; !found {
			// This config resource is not for an adapter (or at least not for one that Mixer is currently aware of).
			continue
		}

		adapterName := key.String()

		log.Debugf("Processing incoming handler config: name='%s'\n%s", adapterName, resource.Spec)

		cfg := &Handler{
			Name:    adapterName,
			Adapter: info,
			Params:  resource.Spec,
		}

		handlers[cfg.Name] = cfg
	}

	counters.handlerConfig.Add(float64(len(handlers)))
	return handlers
}

func (e *Ephemeral) processInstanceConfigs(counters Counters) map[string]*Instance {
	instances := make(map[string]*Instance, len(e.templates))

	for key, resource := range e.entries {
		var info *template.Info
		var found bool
		if info, found = e.templates[key.Kind]; !found {
			// This config resource is not for an instance (or at least not for one that Mixer is currently aware of).
			continue
		}

		instanceName := key.String()

		log.Debugf("Processing incoming instance config: name='%s'\n%s", instanceName, resource.Spec)

		cfg := &Instance{
			Name:     instanceName,
			Template: info,
			Params:   resource.Spec,
		}

		instances[cfg.Name] = cfg
	}

	counters.instanceConfig.Add(float64(len(instances)))
	return instances
}

func (e *Ephemeral) processRuleConfigs(
	handlers map[string]*Handler,
	instances map[string]*Instance,
	counters Counters) []*Rule {

	log.Debug("Begin processing rule configurations.")

	var rules []*Rule

	for ruleKey, resource := range e.entries {
		if ruleKey.Kind != RulesKind {
			continue
		}
		counters.ruleConfig.Add(1)

		ruleName := ruleKey.String()

		cfg := resource.Spec.(*config.Rule)

		log.Debugf("Processing incoming rule: name='%s'\n%s", ruleName, cfg)

		// TODO(Issue #2139): resourceType is used for backwards compatibility with labels: [istio-protocol: tcp]
		// Once that issue is resolved, the following block should be removed.
		rt := resourceType(resource.Metadata.Labels)
		if cfg.Match != "" {
			if m, err := expr.ExtractEQMatches(cfg.Match); err != nil {
				log.Errorf("Unable to extract resource type from rule: name='%s'", ruleName)

				// instead of skipping the rule, add it to the list. This ensures that the behavior will
				// stay the same when this block is removed.
			} else {
				if ContextProtocolTCP == m[ContextProtocolAttributeName] {
					rt.protocol = protocolTCP
				}
			}
		}

		// extract the set of actions from the rule, and the handlers they reference.
		actions := make([]*Action, 0, len(cfg.Actions))
		for i, a := range cfg.Actions {
			log.Debugf("Processing action: %s[%d]", ruleName, i)

			var found bool
			var handler *Handler
			handlerName := canonicalize(a.Handler, ruleKey.Namespace)
			if handler, found = handlers[handlerName]; !found {
				log.Errorf("Handler not found: handler='%s', action='%s[%d]'",
					handlerName, ruleName, i)
				counters.ruleConfigError.Inc()
				continue
			}

			// Keep track of unique instances, to avoid using the same instance multiple times within the same
			// action
			uniqueInstances := make(map[string]bool, len(a.Instances))

			actionInstances := make([]*Instance, 0, len(a.Instances))
			for _, instanceName := range a.Instances {
				instanceName = canonicalize(instanceName, ruleKey.Namespace)
				if _, found = uniqueInstances[instanceName]; found {
					log.Errorf("Action specified the same instance multiple times: action='%s[%d]', instance='%s',",
						ruleName, i, instanceName)
					counters.ruleConfigError.Inc()
					continue
				}
				uniqueInstances[instanceName] = true

				var instance *Instance
				if instance, found = instances[instanceName]; !found {
					log.Errorf("Instance not found: instance='%s', action='%s[%d]'",
						instanceName, ruleName, i)
					counters.ruleConfigError.Inc()
					continue
				}

				actionInstances = append(actionInstances, instance)
			}

			// If there are no valid instances found for this action, then elide the action.
			if len(actionInstances) == 0 {
				log.Errorf("No valid instances found: action='%s[%d]'", ruleName, i)
				counters.ruleConfigError.Inc()
				continue
			}

			action := &Action{
				Handler:   handler,
				Instances: actionInstances,
			}

			actions = append(actions, action)
		}

		// If there are no valid actions found for this rule, then elide the rule.
		if len(actions) == 0 {
			log.Errorf("No valid actions found in rule: %s", ruleName)
			counters.ruleConfigError.Inc()
			continue
		}

		rule := &Rule{
			Name:         ruleName,
			Namespace:    ruleKey.Namespace,
			Actions:      actions,
			ResourceType: rt,
			Match:        cfg.Match,
		}

		rules = append(rules, rule)
	}

	return rules
}

// resourceType maps labels to rule types.
func resourceType(labels map[string]string) ResourceType {
	rt := defaultResourcetype()
	if ContextProtocolTCP == labels[istioProtocol] {
		rt.protocol = protocolTCP
	}
	return rt
}
