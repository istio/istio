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
	"fmt"
	"sync"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/prometheus/client_golang/prometheus"

	"istio.io/api/mixer/adapter/model/v1beta1"
	config "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/config/storetest"
	"istio.io/istio/mixer/pkg/lang/ast"
	"istio.io/istio/mixer/pkg/lang/checker"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/istio/pkg/log"
)

// Ephemeral configuration state that gets updated by incoming config change events. By itself, the data contained
// is not meaningful. BuildSnapshot must be called to create a new snapshot instance, which contains fully resolved
// config.
// The Ephemeral is thread safe, which mean the state can be incrementally built asynchronously, before calling
// BuildSnapshot, using ApplyEvent.
type Ephemeral struct {
	// Static information
	adapters  map[string]*adapter.Info
	templates map[string]*template.Info

	// next snapshot id
	nextID int64

	tc checker.TypeChecker

	// The ephemeral object is used inside a webhooks validators which run as multiple nodes.
	// Which means every ephemeral instance (associated with every isolated webhook node) needs to keep itself in sync with the
	// store's state to do validation of incoming config stanza. The user of the ephemeral must therefore attach the
	// ApplyEvent function to a background store store.WatchChanges callback. Therefore, we need to lock protect the entries
	// because it can get updated either when webhook is invoked for validation or in the background via
	// store.WatchChanges callbacks.
	lock sync.RWMutex // protects resources below

	// entries that are currently known.
	entries map[store.Key]*store.Resource
}

// NewEphemeral returns a new Ephemeral instance.
//
// NOTE: initial state is computed even if there are errors in the config. Configuration that has errors
// is reported in the returned error object, and is ignored in the snapshot creation.
func NewEphemeral(
	templates map[string]*template.Info,
	adapters map[string]*adapter.Info) *Ephemeral {

	e := &Ephemeral{
		templates: templates,
		adapters:  adapters,

		nextID: 0,
		tc:     checker.NewTypeChecker(),

		entries: make(map[store.Key]*store.Resource),
	}

	return e
}

// SetState with the supplied state map. All existing ephemeral state is overwritten.
func (e *Ephemeral) SetState(state map[store.Key]*store.Resource) {
	e.lock.Lock()
	defer e.lock.Unlock()
	e.entries = state
}

// GetEntry returns the value stored for the key in the ephemeral.
func (e *Ephemeral) GetEntry(event *store.Event) (*store.Resource, bool) {
	e.lock.RLock()
	defer e.lock.RUnlock()
	v, ok := e.entries[event.Key]
	return v, ok
}

// ApplyEvent to the internal ephemeral state. This gets called by an external event listener to relay store change
// events to this ephemeral config object.
func (e *Ephemeral) ApplyEvent(events []*store.Event) {
	e.lock.Lock()
	defer e.lock.Unlock()
	for _, event := range events {
		switch event.Type {
		case store.Update:
			e.entries[event.Key] = event.Value
		case store.Delete:
			delete(e.entries, event.Key)
		}
	}
}

// BuildSnapshot builds a stable, fully-resolved snapshot view of the configuration.
func (e *Ephemeral) BuildSnapshot() (*Snapshot, error) {
	errs := &multierror.Error{}
	id := e.nextID
	e.nextID++

	log.Debugf("Building new config.Snapshot: id='%d'", id)

	// Allocate new counters, to use with the new snapshot.
	counters := newCounters(id)

	e.lock.RLock()

	attributes := e.processAttributeManifests(counters, errs)

	handlers := e.processHandlerConfigs(counters, errs)

	af := ast.NewFinder(attributes)
	instances := e.processInstanceConfigs(af, counters, errs)
	adapterInfos := e.processAdapterInfoConfigs(counters, errs)

	rules := e.processRuleConfigs(handlers, instances, af, counters, errs)
	s := &Snapshot{
		ID:                id,
		Templates:         e.templates,
		Adapters:          e.adapters,
		TemplateMetadatas: adapterInfos.templates,
		AdapterMetadatas:  adapterInfos.adapters,
		Attributes:        ast.NewFinder(attributes),
		HandlersLegacy:    handlers,
		InstancesLegacy:   instances,
		RulesLegacy:       rules,
		Counters:          counters,
	}
	e.lock.RUnlock()

	log.Infof("Built new config.Snapshot: id='%d'", id)
	log.Debugf("config.Snapshot creation error=%v, contents:\n%s", errs.ErrorOrNil(), s)
	return s, errs.ErrorOrNil()
}

func (e *Ephemeral) processAttributeManifests(counters Counters, errs *multierror.Error) map[string]*config.AttributeManifest_AttributeInfo {
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

func (e *Ephemeral) processHandlerConfigs(counters Counters, errs *multierror.Error) map[string]*HandlerLegacy {
	handlers := make(map[string]*HandlerLegacy, len(e.adapters))

	for key, resource := range e.entries {
		var info *adapter.Info
		var found bool
		if info, found = e.adapters[key.Kind]; !found {
			// This config resource is not for an adapter (or at least not for one that Mixer is currently aware of).
			continue
		}

		adapterName := key.String()

		log.Debugf("Processing incoming handler config: name='%s'\n%s", adapterName, resource.Spec)

		cfg := &HandlerLegacy{
			Name:    adapterName,
			Adapter: info,
			Params:  resource.Spec,
		}

		handlers[cfg.Name] = cfg
	}

	counters.handlerConfig.Add(float64(len(handlers)))
	return handlers
}

func (e *Ephemeral) processInstanceConfigs(attributes ast.AttributeDescriptorFinder, counters Counters,
	errs *multierror.Error) map[string]*InstanceLegacy {
	instances := make(map[string]*InstanceLegacy, len(e.templates))

	for key, resource := range e.entries {
		var info *template.Info
		var found bool
		if info, found = e.templates[key.Kind]; !found {
			// This config resource is not for an instance (or at least not for one that Mixer is currently aware of).
			continue
		}

		instanceName := key.String()

		log.Debugf("Processing incoming instance config: name='%s'\n%s", instanceName, resource.Spec)
		inferredType, err := info.InferType(resource.Spec, func(s string) (config.ValueType, error) {
			return e.tc.EvalType(s, attributes)
		})
		if err != nil {
			appendErr(errs, fmt.Sprintf("instance='%s'", instanceName), counters.instanceConfigError, err.Error())
			continue
		}
		cfg := &InstanceLegacy{
			Name:         instanceName,
			Template:     info,
			Params:       resource.Spec,
			InferredType: inferredType,
		}

		instances[cfg.Name] = cfg
	}

	counters.instanceConfig.Add(float64(len(instances)))
	return instances
}

func (e *Ephemeral) processAdapterInfoConfigs(counters Counters, errs *multierror.Error) *adapterInfoRegistry {

	log.Debug("Begin processing adapter info configurations.")

	var adapterInfos []*v1beta1.Info

	for adapterInfoKey, resource := range e.entries {
		if adapterInfoKey.Kind != AdapterKind {
			continue
		}
		counters.adapterInfoConfig.Add(1)
		cfg := resource.Spec.(*v1beta1.Info)
		log.Debugf("Processing incoming adapter info: name='%s'\n%s", adapterInfoKey.String(), cfg)
		adapterInfos = append(adapterInfos, cfg)
	}

	log.Debugf("Total received adapter info: count=%d, value='%v'", len(adapterInfos), adapterInfos)
	reg, err := newAdapterInfoRegistry(adapterInfos)
	if err != nil {
		log.Errorf("Error when reading adapter info='%v'", err)
		counters.adapterInfoConfigError.Inc()
	}
	log.Debugf("Total successfully ingested templates: count=%d, value='%v'", len(reg.templates), reg.templates)
	log.Debugf("Total successfully ingested adapters: count=%d, value='%v'", len(reg.adapters), reg.adapters)
	return reg
}

func (e *Ephemeral) processRuleConfigs(

	handlers map[string]*HandlerLegacy,
	instances map[string]*InstanceLegacy,
	attributes ast.AttributeDescriptorFinder,
	counters Counters, errs *multierror.Error) []*RuleLegacy {

	log.Debug("Begin processing rule configurations.")

	var rules []*RuleLegacy

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
			if err := e.tc.AssertType(cfg.Match, attributes, config.BOOL); err != nil {
				appendErr(errs, fmt.Sprintf("rule='%s'.Match", ruleName), counters.ruleConfigError, err.Error())
			}

			if m, err := ast.ExtractEQMatches(cfg.Match); err != nil {
				appendErr(errs, fmt.Sprintf("rule='%s'", ruleName), counters.ruleConfigError,
					"Unable to extract resource type from rule")
				// instead of skipping the rule, add it to the list. This ensures that the behavior will
				// stay the same when this block is removed.
			} else {
				if ContextProtocolTCP == m[ContextProtocolAttributeName] {
					rt.protocol = protocolTCP
				}
			}
		}

		// extract the set of actions from the rule, and the handlers they reference.
		actions := make([]*ActionLegacy, 0, len(cfg.Actions))
		for i, a := range cfg.Actions {
			log.Debugf("Processing action: %s[%d]", ruleName, i)

			var found bool
			var handler *HandlerLegacy
			handlerName := canonicalize(a.Handler, ruleKey.Namespace)
			if handler, found = handlers[handlerName]; !found {
				appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i), counters.ruleConfigError, "Handler not found: handler='%s'",
					handlerName)
				continue
			}

			// Keep track of unique instances, to avoid using the same instance multiple times within the same
			// action
			uniqueInstances := make(map[string]bool, len(a.Instances))

			actionInstances := make([]*InstanceLegacy, 0, len(a.Instances))
			for _, instanceName := range a.Instances {
				instanceName = canonicalize(instanceName, ruleKey.Namespace)
				if _, found = uniqueInstances[instanceName]; found {
					appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i), counters.ruleConfigError,
						"action specified the same instance multiple times: instance='%s',", instanceName)
					continue
				}
				uniqueInstances[instanceName] = true

				var instance *InstanceLegacy
				if instance, found = instances[instanceName]; !found {
					appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i), counters.ruleConfigError, "Instance not found: instance='%s'", instanceName)
					continue
				}

				actionInstances = append(actionInstances, instance)
			}

			// If there are no valid instances found for this action, then elide the action.
			if len(actionInstances) == 0 {
				appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i), counters.ruleConfigError, "No valid instances found")
				continue
			}

			action := &ActionLegacy{
				Handler:   handler,
				Instances: actionInstances,
			}

			actions = append(actions, action)
		}

		// If there are no valid actions found for this rule, then elide the rule.
		if len(actions) == 0 {
			appendErr(errs, fmt.Sprintf("rule=%s", ruleName), counters.ruleConfigError, "No valid actions found in rule")
			continue
		}

		rule := &RuleLegacy{
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

func appendErr(errs *multierror.Error, field string, counter prometheus.Counter, format string, a ...interface{}) {
	err := fmt.Errorf(format, a...)
	log.Error(err.Error())
	counter.Inc()
	_ = multierror.Append(errs, adapter.ConfigError{Field: field, Underlying: err})
}

// resourceType maps labels to rule types.
func resourceType(labels map[string]string) ResourceType {
	rt := defaultResourcetype()
	if ContextProtocolTCP == labels[istioProtocol] {
		rt.protocol = protocolTCP
	}
	return rt
}

// GetSnapshotForTest creates a config.Snapshot for testing purposes, based on the supplied configuration.
func GetSnapshotForTest(templates map[string]*template.Info, adapters map[string]*adapter.Info, serviceConfig string, globalConfig string) (*Snapshot, error) {
	store, _ := storetest.SetupStoreForTest(serviceConfig, globalConfig)

	_ = store.Init(KindMap(adapters, templates))

	data := store.List()

	// NewEphemeral tries to build a snapshot with empty entries therefore it never fails; Ignoring the error.
	e := NewEphemeral(templates, adapters)

	e.SetState(data)

	store.Stop()

	return e.BuildSnapshot()
}
