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

// Package config is designed to listen to the config changes through the store and create a fully-resolved configuration
// state that can be used by the rest of the runtime code.
//
// The main purpose of this library is to create an object-model that simplifies queries and correctness checks that
// the client code needs to deal with. This is accomplished by making sure the config state is fully resolved, and
// incorporating otherwise complex queries within this package.
package config

import (
	"fmt"
	"sync"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	"github.com/gogo/protobuf/types"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/prometheus/client_golang/prometheus"

	"istio.io/api/mixer/adapter/model/v1beta1"
	config "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/config/storetest"
	"istio.io/istio/mixer/pkg/lang/ast"
	"istio.io/istio/mixer/pkg/lang/checker"
	"istio.io/istio/mixer/pkg/lang/compiled"
	"istio.io/istio/mixer/pkg/protobuf/yaml"
	"istio.io/istio/mixer/pkg/protobuf/yaml/dynamic"
	"istio.io/istio/mixer/pkg/runtime/config/constant"
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

	shandlers := e.processStaticAdapterHandlerConfigs(counters, errs)

	af := ast.NewFinder(attributes)
	instances := e.processInstanceConfigs(af, counters, errs)

	// New dynamic configurations
	dTemplates := e.processDynamicTemplateConfigs(counters, errs)
	dAdapters := e.processDynamicAdapterConfigs(dTemplates, counters, errs)
	dhandlers := e.processDynamicHandlerConfigs(dAdapters, counters, errs)
	dInstances := e.processDynamicInstanceConfigs(dTemplates, af, counters, errs)

	rules := e.processRuleConfigs(shandlers, instances, dhandlers, dInstances, af, counters, errs)

	s := &Snapshot{
		ID:                id,
		Templates:         e.templates,
		Adapters:          e.adapters,
		TemplateMetadatas: dTemplates,
		AdapterMetadatas:  dAdapters,
		Attributes:        ast.NewFinder(attributes),
		HandlersStatic:    shandlers,
		InstancesStatic:   instances,
		Rules:             rules,

		HandlersDynamic:  dhandlers,
		InstancesDynamic: dInstances,

		Counters: counters,
	}
	e.lock.RUnlock()

	log.Infof("Built new config.Snapshot: id='%d'", id)
	log.Debugf("config.Snapshot creation error=%v, contents:\n%s", errs.ErrorOrNil(), s)
	return s, errs.ErrorOrNil()
}

func (e *Ephemeral) processAttributeManifests(counters Counters, errs *multierror.Error) map[string]*config.AttributeManifest_AttributeInfo {
	attrs := make(map[string]*config.AttributeManifest_AttributeInfo)
	for k, obj := range e.entries {
		if k.Kind != constant.AttributeManifestKind {
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

func (e *Ephemeral) processStaticAdapterHandlerConfigs(counters Counters, errs *multierror.Error) map[string]*HandlerStatic {
	handlers := make(map[string]*HandlerStatic, len(e.adapters))

	for key, resource := range e.entries {
		var info *adapter.Info
		var found bool
		if info, found = e.adapters[key.Kind]; !found {
			// This config resource is not for an adapter (or at least not for one that Mixer is currently aware of).
			continue
		}

		adapterName := key.String()

		log.Debugf("Processing incoming handler config: name='%s'\n%s", adapterName, resource.Spec)

		cfg := &HandlerStatic{
			Name:    adapterName,
			Adapter: info,
			Params:  resource.Spec,
		}

		handlers[cfg.Name] = cfg
	}

	counters.handlerConfig.Add(float64(len(handlers)))
	return handlers
}

func (e *Ephemeral) processDynamicHandlerConfigs(adapters map[string]*Adapter, counters Counters, errs *multierror.Error) map[string]*HandlerDynamic {
	handlers := make(map[string]*HandlerDynamic, len(e.adapters))

	for key, resource := range e.entries {
		isHandler, hasStaticAdapter := e.isHandler(key, resource.Spec)
		if !isHandler || hasStaticAdapter {
			// static adapter based handlers are processed elsewhere
			continue
		}

		hdl := resource.Spec.(*config.Handler)
		an1, an2 := canonicalize(hdl.Adapter, constant.AdapterKind, key.Namespace)
		handlerName := key.String()
		log.Debugf("Processing incoming handler config: name='%s'\n%s", handlerName, resource.Spec)

		adapter := adapters[an1]
		if adapter == nil {
			adapter = adapters[an2]
		}

		if adapter == nil {
			appendErr(errs, fmt.Sprintf("handler='%s'.adapter", handlerName),
				counters.HandlerValidationError, "adapter '%s' not found", hdl.Adapter)
			continue
		}
		// validate if the param is valid
		bytes, err := validateEncodeBytes(hdl.Params, adapter.ConfigDescSet, getParamsMsgFullName(adapter.PackageName))
		if err != nil {
			appendErr(errs, fmt.Sprintf("handler='%s'.params", handlerName),
				counters.HandlerValidationError, err.Error())
			continue
		}

		adapterCfg := &types.Any{
			TypeUrl: adapter.PackageName + ".Params",
			Value:   bytes,
		}

		cfg := &HandlerDynamic{
			Name:          handlerName,
			Adapter:       adapter,
			Connection:    hdl.Connection,
			AdapterConfig: adapterCfg,
		}

		handlers[cfg.Name] = cfg
	}

	counters.handlerConfig.Add(float64(len(handlers)))
	return handlers
}

func (e *Ephemeral) processDynamicInstanceConfigs(templates map[string]*Template,
	attributes ast.AttributeDescriptorFinder, counters Counters, errs *multierror.Error) map[string]*InstanceDynamic {
	instances := make(map[string]*InstanceDynamic, len(e.templates))

	for key, resource := range e.entries {
		isInstance, hasStaticTemplate := e.isInstance(key, resource.Spec)
		if !isInstance || hasStaticTemplate {
			// static template based instances are processed elsewhere
			continue
		}

		inst := resource.Spec.(*config.Instance)
		tn1, tn2 := canonicalize(inst.Template, constant.TemplateKind, key.Namespace)
		instanceName := key.String()
		log.Debugf("Processing incoming instance config: name='%s'\n%s", instanceName, resource.Spec)

		template := templates[tn1]
		if template == nil {
			template = templates[tn2]
		}

		if template == nil {
			appendErr(errs, fmt.Sprintf("instance='%s'.template", instanceName),
				counters.instanceConfigError, "template '%s' not found", inst.Template)
			continue
		}

		// validate if the param is valid
		compiler := compiled.NewBuilder(attributes)
		resolver := yaml.NewResolver(template.FileDescSet)
		b := dynamic.NewEncoderBuilder(
			resolver,
			compiler,
			false)
		var enc dynamic.Encoder
		var params map[string]interface{}
		var err error
		if inst.Params != nil {
			params = inst.Params.(map[string]interface{})
			params["name"] = fmt.Sprintf("\"%s\"", instanceName)
			enc, err = b.Build(getTemplatesMsgFullName(template.PackageName), params)
			if err != nil {
				appendErr(errs, fmt.Sprintf("instance='%s'.params", instanceName),
					counters.instanceConfigError, "config does not conforms to schema of template '%s': %v",
					inst.Template, err.Error())
				continue
			}
		}

		cfg := &InstanceDynamic{
			Name:     instanceName,
			Template: template,
			Encoder:  enc,
			Params:   params,
		}

		instances[cfg.Name] = cfg
	}

	counters.instanceConfig.Add(float64(len(instances)))
	return instances
}

func getTemplatesMsgFullName(pkgName string) string {
	return "." + pkgName + ".InstanceMsg"
}

func getParamsMsgFullName(pkgName string) string {
	return "." + pkgName + ".Params"
}

func (e *Ephemeral) isHandler(key store.Key, spec proto.Message) (isHandler bool, hasStaticAdapter bool) {
	if key.Kind != constant.HandlerKind {
		return
	}
	isHandler = true
	hdl := spec.(*config.Handler)
	if _, found := e.adapters[hdl.Adapter]; found {
		hasStaticAdapter = true
	}
	return
}

func (e *Ephemeral) isInstance(key store.Key, spec proto.Message) (isInstance bool, hasStaticTemplate bool) {
	if key.Kind != constant.InstanceKind {
		return
	}
	isInstance = true
	inst := spec.(*config.Instance)
	if _, found := e.templates[inst.Template]; found {
		hasStaticTemplate = true
	}
	return
}

func validateEncodeBytes(params interface{}, fds *descriptor.FileDescriptorSet, msgName string) ([]byte, error) {
	if params == nil {
		return []byte{}, nil
	}
	return yaml.NewEncoder(fds).EncodeBytes(params.(map[string]interface{}), msgName, false)
}

func (e *Ephemeral) processInstanceConfigs(attributes ast.AttributeDescriptorFinder, counters Counters,
	errs *multierror.Error) map[string]*InstanceStatic {
	instances := make(map[string]*InstanceStatic, len(e.templates))

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
		cfg := &InstanceStatic{
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

func (e *Ephemeral) processDynamicAdapterConfigs(availableTmpls map[string]*Template, counters Counters, errs *multierror.Error) map[string]*Adapter {
	result := map[string]*Adapter{}
	log.Debug("Begin processing adapter info configurations.")
	for adapterInfoKey, resource := range e.entries {
		if adapterInfoKey.Kind != constant.AdapterKind {
			continue
		}

		adapterName := adapterInfoKey.String()

		counters.adapterInfoConfig.Add(1)
		cfg := resource.Spec.(*v1beta1.Info)

		log.Debugf("Processing incoming adapter info: name='%s'\n%v", adapterName, cfg)

		if _, ok := e.adapters[adapterInfoKey.Name]; ok {
			// compiled in adapter already contains a same named adapter as key.Name.
			// This will cause confusion to operator referencing the adapter, so we should disallow this
			// and let operator rename their adapter's CR name to disambiguate.
			appendErr(errs, fmt.Sprintf("adapter='%s'", adapterInfoKey.Name), counters.adapterInfoConfigError,
				"same named adapter already exists inside mixer; please rename to disambiguate")
			continue
		}

		fds, desc, err := GetAdapterCfgDescriptor(cfg.Config)
		if err != nil {
			appendErr(errs, fmt.Sprintf("adapter='%s'", adapterName), counters.adapterInfoConfigError,
				"unable to parse adapter configuration: %v", err)
			continue
		}
		supportedTmpls := make([]*Template, 0)
		for _, tmplN := range cfg.Templates {
			tn1, tn2 := canonicalize(tmplN, constant.TemplateKind, adapterInfoKey.Namespace)
			tmplFullName := tn1
			if _, ok := availableTmpls[tn1]; !ok {
				tmplFullName = tn2
				if _, ok := availableTmpls[tn2]; !ok {
					appendErr(errs, fmt.Sprintf("adapter='%s'", adapterName), counters.adapterInfoConfigError,
						"unable to find template '%s'", tmplN)
					continue
				}
			}
			supportedTmpls = append(supportedTmpls, availableTmpls[tmplFullName])
		}
		if len(cfg.Templates) == len(supportedTmpls) {
			// only record adapter if all templates are valid
			result[adapterName] = &Adapter{
				Name:               adapterName,
				ConfigDescSet:      fds,
				PackageName:        desc.GetPackage(),
				SupportedTemplates: supportedTmpls,
				SessionBased:       cfg.SessionBased,
				Description:        cfg.Description,
			}
		}
	}
	return result
}

func (e *Ephemeral) processRuleConfigs(
	sHandlers map[string]*HandlerStatic,
	sInstances map[string]*InstanceStatic,
	dHandlers map[string]*HandlerDynamic,
	dInstances map[string]*InstanceDynamic,
	attributes ast.AttributeDescriptorFinder,
	counters Counters, errs *multierror.Error) []*Rule {

	log.Debug("Begin processing rule configurations.")

	var rules []*Rule

	for ruleKey, resource := range e.entries {
		if ruleKey.Kind != constant.RulesKind {
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
				if constant.ContextProtocolTCP == m[constant.ContextProtocolAttributeName] {
					rt.protocol = protocolTCP
				}
			}
		}

		// extract the set of actions from the rule, and the handlers they reference.
		// A rule can have both static and dynamic actions.

		actionsStat := make([]*ActionStatic, 0, len(cfg.Actions))
		actionsDynamic := make([]*ActionDynamic, 0, len(cfg.Actions))
		for i, a := range cfg.Actions {
			log.Debugf("Processing action: %s[%d]", ruleName, i)
			var isHandlerStat bool
			var isHandlerDynamic bool
			var sahandler *HandlerStatic
			var dahandler *HandlerDynamic
			hn1, hn2 := canonicalize(a.Handler, constant.HandlerKind, ruleKey.Namespace)

			handlerName := hn1
			if sahandler, isHandlerStat = sHandlers[hn1]; !isHandlerStat {
				handlerName = hn2
				sahandler, isHandlerStat = sHandlers[hn2]
			}

			if !isHandlerStat {
				handlerName = hn1
				if dahandler, isHandlerDynamic = dHandlers[hn1]; !isHandlerDynamic {
					handlerName = hn2
					dahandler, isHandlerDynamic = dHandlers[hn2]
				}
			}

			if !isHandlerStat && !isHandlerDynamic {
				appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i), counters.ruleConfigError,
					"Handler not found: handler='%s'", a.Handler)
				continue
			}

			if isHandlerStat {
				// Keep track of unique instances, to avoid using the same instance multiple times within the same
				// action
				uniqueInstances := make(map[string]bool, len(a.Instances))

				actionInstances := make([]*InstanceStatic, 0, len(a.Instances))
				for _, instanceNameRef := range a.Instances {
					in1, in2 := canonicalize(instanceNameRef, constant.InstanceKind, ruleKey.Namespace)

					var instance *InstanceStatic
					var found bool
					instName := in1
					if instance, found = sInstances[in1]; !found {
						instName = in2
						if instance, found = sInstances[in2]; !found {
							appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i), counters.ruleConfigError,
								"Instance not found: instance='%s'", instanceNameRef)
							continue
						}
					}

					if _, ok := uniqueInstances[instName]; ok {
						appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i), counters.ruleConfigError,
							"action specified the same instance multiple times: instance='%s',", instName)
						continue
					}
					uniqueInstances[instName] = true

					if !contains(sahandler.Adapter.SupportedTemplates, instance.Template.Name) {
						appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i), counters.ruleConfigError,
							"instance '%s' is of template '%s' which is not supported by handler '%s'",
							instName, instance.Template.Name, handlerName)
						continue
					}

					actionInstances = append(actionInstances, instance)
				}

				// If there are no valid instances found for this action, then elide the action.
				if len(actionInstances) == 0 {
					appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i), counters.ruleConfigError, "No valid instances found")
					continue
				}

				action := &ActionStatic{
					Handler:   sahandler,
					Instances: actionInstances,
				}

				actionsStat = append(actionsStat, action)
			} else {
				// Keep track of unique instances, to avoid using the same instance multiple times within the same
				// action
				uniqueInstances := make(map[string]bool, len(a.Instances))

				actionInstances := make([]*InstanceDynamic, 0, len(a.Instances))
				for _, instanceNameRef := range a.Instances {
					in1, in2 := canonicalize(instanceNameRef, constant.InstanceKind, ruleKey.Namespace)

					var instance *InstanceDynamic
					var instfound bool
					instName := in1
					if instance, instfound = dInstances[in1]; !instfound {
						instName = in2
						if instance, instfound = dInstances[in2]; !instfound {
							appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i), counters.ruleConfigError, "Instance not found: instance='%s'", instanceNameRef)
							continue
						}
					}

					if _, ok := uniqueInstances[instName]; ok {
						appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i), counters.ruleConfigError,
							"action specified the same instance multiple times: instance='%s',", instName)
						continue
					}
					uniqueInstances[instName] = true

					found := false
					for _, supTmpl := range dahandler.Adapter.SupportedTemplates {
						if supTmpl.Name == instance.Template.Name {
							found = true
							break
						}
					}

					if !found {
						appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i), counters.ruleConfigError,
							"instance '%s' is of template '%s' which is not supported by handler '%s'",
							instName, instance.Template.Name, handlerName)
						continue
					}

					actionInstances = append(actionInstances, instance)
				}

				// If there are no valid instances found for this action, then elide the action.
				if len(actionInstances) == 0 {
					appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i), counters.ruleConfigError, "No valid instances found")
					continue
				}

				action := &ActionDynamic{
					Handler:   dahandler,
					Instances: actionInstances,
				}

				actionsDynamic = append(actionsDynamic, action)
			}
		}

		// If there are no valid actions found for this rule, then elide the rule.
		if len(actionsStat) == 0 && len(actionsDynamic) == 0 {
			appendErr(errs, fmt.Sprintf("rule=%s", ruleName), counters.ruleConfigError, "No valid actions found in rule")
			continue
		}

		rule := &Rule{
			Name:           ruleName,
			Namespace:      ruleKey.Namespace,
			ActionsStatic:  actionsStat,
			ActionsDynamic: actionsDynamic,
			ResourceType:   rt,
			Match:          cfg.Match,
		}

		rules = append(rules, rule)
	}

	return rules
}

func contains(strs []string, w string) bool {
	for _, v := range strs {
		if v == w {
			return true
		}
	}
	return false
}

func (e *Ephemeral) processDynamicTemplateConfigs(counters Counters, errs *multierror.Error) map[string]*Template {
	result := map[string]*Template{}
	log.Debug("Begin processing templates.")
	for templateKey, resource := range e.entries {
		if templateKey.Kind != constant.TemplateKind {
			continue
		}
		counters.templateConfig.Add(1)

		templateName := templateKey.String()
		cfg := resource.Spec.(*v1beta1.Template)
		log.Debugf("Processing incoming template: name='%s'\n%v", templateName, cfg)

		if _, ok := e.templates[templateKey.Name]; ok {
			// compiled in templates already contains a same named template as templateKey.Name.
			// This will cause confusion to operator referencing the template, so we should disallow this
			// and let operator rename their template's CR name to disambiguate.
			appendErr(errs, fmt.Sprintf("template='%s'", templateKey.Name), counters.templateConfigError,
				"same named template already exists inside mixer; please rename to disambiguate")
			continue
		}
		fds, desc, name, variety, err := GetTmplDescriptor(cfg.Descriptor_)
		if err != nil {
			appendErr(errs, fmt.Sprintf("template='%s'", templateName), counters.templateConfigError,
				"unable to parse descriptor: %v", err)
			continue
		}

		result[templateName] = &Template{
			Name: templateName,
			InternalPackageDerivedName: name,
			FileDescSet:                fds,
			PackageName:                desc.GetPackage(),
			Variety:                    variety,
		}
	}
	return result
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
	if constant.ContextProtocolTCP == labels[constant.IstioProtocol] {
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
