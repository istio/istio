// Copyright Istio Authors
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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	"github.com/gogo/protobuf/types"
	multierror "github.com/hashicorp/go-multierror"
	"go.opencensus.io/stats"

	"istio.io/api/mixer/adapter/model/v1beta1"
	config "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/config/storetest"
	"istio.io/istio/mixer/pkg/lang/checker"
	"istio.io/istio/mixer/pkg/protobuf/yaml"
	"istio.io/istio/mixer/pkg/protobuf/yaml/dynamic"
	"istio.io/istio/mixer/pkg/runtime/config/constant"
	"istio.io/istio/mixer/pkg/runtime/lang"
	"istio.io/istio/mixer/pkg/runtime/monitoring"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/pkg/attribute"
	"istio.io/pkg/log"
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

	// type checkers
	attributes attribute.AttributeDescriptorFinder
	tcs        map[lang.LanguageRuntime]lang.TypeChecker

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
		tcs:    make(map[lang.LanguageRuntime]checker.TypeChecker),

		entries: make(map[store.Key]*store.Resource),
	}

	return e
}

func (e *Ephemeral) checker(mode lang.LanguageRuntime) checker.TypeChecker {
	if out, ok := e.tcs[mode]; ok {
		return out
	}

	out := lang.NewTypeChecker(e.attributes, mode)
	e.tcs[mode] = out
	return out
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

	// Allocate new monitoring context to use with the new snapshot.
	monitoringCtx := context.Background()

	e.lock.RLock()

	attributes := e.processAttributeManifests(monitoringCtx)

	shandlers := e.processStaticAdapterHandlerConfigs()

	af := attribute.NewFinder(attributes)
	e.attributes = af
	instances, instErrs := e.processInstanceConfigs(errs)

	// New dynamic configurations
	dTemplates := e.processDynamicTemplateConfigs(monitoringCtx, errs)
	dAdapters := e.processDynamicAdapterConfigs(monitoringCtx, dTemplates, errs)
	dhandlers := e.processDynamicHandlerConfigs(monitoringCtx, dAdapters, errs)
	dInstances, dInstErrs := e.processDynamicInstanceConfigs(dTemplates, errs)

	rules := e.processRuleConfigs(monitoringCtx, shandlers, instances, dhandlers, dInstances, errs)

	stats.Record(monitoringCtx,
		monitoring.HandlersTotal.M(int64(len(shandlers)+len(dhandlers))),
		monitoring.InstancesTotal.M(int64(len(instances)+len(dInstances))),
		monitoring.RulesTotal.M(int64(len(rules))),
		monitoring.AdapterInfosTotal.M(int64(len(dAdapters))),
		monitoring.TemplatesTotal.M(int64(len(dTemplates))),
		monitoring.InstanceErrs.M(instErrs+dInstErrs),
	)

	s := &Snapshot{
		ID:                id,
		Templates:         e.templates,
		Adapters:          e.adapters,
		TemplateMetadatas: dTemplates,
		AdapterMetadatas:  dAdapters,
		Attributes:        af,
		HandlersStatic:    shandlers,
		InstancesStatic:   instances,
		Rules:             rules,

		HandlersDynamic:  dhandlers,
		InstancesDynamic: dInstances,

		MonitoringContext: monitoringCtx,
	}
	e.lock.RUnlock()

	log.Debugf("config.Snapshot creation error=%v, contents:\n%s", errs.ErrorOrNil(), s)
	return s, errs.ErrorOrNil()
}

func (e *Ephemeral) processAttributeManifests(ctx context.Context) map[string]*config.AttributeManifest_AttributeInfo {
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

	// append all the well known attribute vocabulary from the static templates.
	//
	// ATTRIBUTE_GENERATOR variety templates allow operators to write Attributes
	// using the $out.<field Name> convention, where $out refers to the output object from the attribute generating adapter.
	// The list of valid names for a given Template is available in the Template.Info.AttributeManifests object.
	for _, info := range e.templates {
		if info.Variety != v1beta1.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR {
			continue
		}

		log.Debugf("Processing attributes from template: '%s'", info.Name)

		for _, v := range info.AttributeManifests {
			for an, at := range v.Attributes {
				attrs[an] = at
				log.Debugf("Attribute '%s': '%s'", an, at.ValueType)
			}
		}
	}

	log.Debug("Completed processing attributes.")
	stats.Record(ctx, monitoring.AttributesTotal.M(int64(len(attrs))))
	return attrs
}

// convert converts unstructured spec into the target proto.
func convert(spec map[string]interface{}, target proto.Message) error {
	jsonData, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	if err = jsonpb.Unmarshal(bytes.NewReader(jsonData), target); err != nil {
		log.Warnf("unable to unmarshal: %s, %s", err.Error(), string(jsonData))
	}
	return err
}

func (e *Ephemeral) processStaticAdapterHandlerConfigs() map[string]*HandlerStatic {
	handlers := make(map[string]*HandlerStatic, len(e.adapters))

	for key, resource := range e.entries {
		var info *adapter.Info
		var found bool

		if key.Kind == constant.HandlerKind {
			log.Debugf("Static Handler: %#v (name: %s)", key, key.Name)
			handlerProto := resource.Spec.(*config.Handler)
			a, ok := e.adapters[handlerProto.CompiledAdapter]
			if !ok {
				continue
			}
			staticConfig := &HandlerStatic{
				// example: stdio.istio-system
				Name:    key.Name + "." + key.Namespace,
				Adapter: a,
				Params:  a.DefaultConfig,
			}
			if handlerProto.Params != nil {
				c := proto.Clone(staticConfig.Adapter.DefaultConfig)
				if handlerProto.GetParams() != nil {
					dict, err := toDictionary(handlerProto.Params)
					if err != nil {
						log.Warnf("could not convert handler params; using default config: %v", err)
					} else if err := convert(dict, c); err != nil {
						log.Warnf("could not convert handler params; using default config: %v", err)
					}
				}
				staticConfig.Params = c
			}
			handlers[key.String()] = staticConfig
			continue
		}

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

	return handlers
}

func getCanonicalRef(n, kind, ns string, lookup func(string) interface{}) (interface{}, string) {
	name, altName := canonicalize(n, kind, ns)
	v := lookup(name)
	if v != nil {
		return v, name
	}

	return lookup(altName), altName
}

func (e *Ephemeral) processDynamicHandlerConfigs(ctx context.Context, adapters map[string]*Adapter, errs *multierror.Error) map[string]*HandlerDynamic {
	handlers := make(map[string]*HandlerDynamic, len(e.adapters))
	var validationErrs int64

	for key, resource := range e.entries {
		if key.Kind != constant.HandlerKind {
			continue
		}

		handlerName := key.String()
		log.Debugf("Processing incoming handler config: name='%s'\n%s", handlerName, resource.Spec)

		hdl := resource.Spec.(*config.Handler)
		if len(hdl.CompiledAdapter) > 0 {
			continue // this will have already been added in processStaticHandlerConfigs
		}
		adpt, _ := getCanonicalRef(hdl.Adapter, constant.AdapterKind, key.Namespace, func(n string) interface{} {
			if a, ok := adapters[n]; ok {
				return a
			}
			return nil
		})

		if adpt == nil {
			validationErrs++
			appendErr(errs, fmt.Sprintf("handler='%s'.adapter", handlerName), "adapter '%s' not found", hdl.Adapter)
			continue
		}
		adapter := adpt.(*Adapter)

		var adapterCfg *types.Any
		if len(adapter.ConfigDescSet.File) != 0 {
			// validate if the param is valid
			bytes, err := validateEncodeBytes(hdl.Params, adapter.ConfigDescSet, getParamsMsgFullName(adapter.PackageName))
			if err != nil {
				validationErrs++
				appendErr(errs, fmt.Sprintf("handler='%s'.params", handlerName), err.Error())
				continue
			}
			typeFQN := adapter.PackageName + ".Params"
			adapterCfg = asAny(typeFQN, bytes)
		}

		cfg := &HandlerDynamic{
			Name:          handlerName,
			Adapter:       adapter,
			Connection:    hdl.Connection,
			AdapterConfig: adapterCfg,
		}

		handlers[cfg.Name] = cfg
	}

	stats.Record(ctx, monitoring.HandlerValidationErrors.M(validationErrs))

	return handlers
}

const googleApis = "type.googleapis.com/"

func asAny(msgFQN string, bytes []byte) *types.Any {
	return &types.Any{
		TypeUrl: googleApis + msgFQN,
		Value:   bytes,
	}
}

func (e *Ephemeral) processDynamicInstanceConfigs(templates map[string]*Template, errs *multierror.Error) (map[string]*InstanceDynamic, int64) {
	instances := make(map[string]*InstanceDynamic, len(e.templates))
	var instErrs int64

	for key, resource := range e.entries {
		if key.Kind != constant.InstanceKind {
			continue
		}

		inst := resource.Spec.(*config.Instance)

		// should be processed as static instance
		if inst.CompiledTemplate != "" {
			continue
		}

		instanceName := key.String()
		log.Debugf("Processing incoming instance config: name='%s'\n%s", instanceName, resource.Spec)

		tmpl, _ := getCanonicalRef(inst.Template, constant.TemplateKind, key.Namespace, func(n string) interface{} {
			if a, ok := templates[n]; ok {
				return a
			}
			return nil
		})

		if tmpl == nil {
			instErrs++
			appendErr(errs, fmt.Sprintf("instance='%s'.template", instanceName), "template '%s' not found", inst.Template)
			continue
		}

		template := tmpl.(*Template)
		// validate if the param is valid
		mode := lang.GetLanguageRuntime(resource.Metadata.Annotations)
		compiler := lang.NewBuilder(e.attributes, mode)
		resolver := yaml.NewResolver(template.FileDescSet)
		b := dynamic.NewEncoderBuilder(
			resolver,
			compiler,
			false)
		var enc dynamic.Encoder
		var params map[string]interface{}
		var err error
		if inst.Params != nil {
			if params, err = toDictionary(inst.Params); err != nil {
				instErrs++
				appendErr(errs, fmt.Sprintf("instance='%s'.params", instanceName), "invalid params block.")
				continue
			}

			// name field is not provided by instance config author, instead it is added by Mixer into the request
			// object that is passed to the adapter.
			params["name"] = fmt.Sprintf("\"%s\"", instanceName)
			enc, err = b.Build(getTemplatesMsgFullName(template.PackageName), params)
			if err != nil {
				instErrs++
				appendErr(errs, fmt.Sprintf("instance='%s'.params", instanceName),
					"config does not conform to schema of template '%s': %v", inst.Template, err.Error())
				continue
			}
		}

		cfg := &InstanceDynamic{
			Name:              instanceName,
			Template:          template,
			Encoder:           enc,
			Params:            params,
			AttributeBindings: inst.AttributeBindings,
			Language:          mode,
		}

		instances[cfg.Name] = cfg
	}

	return instances, instErrs
}

func getTemplatesMsgFullName(pkgName string) string {
	return "." + pkgName + ".InstanceMsg"
}

func getParamsMsgFullName(pkgName string) string {
	return "." + pkgName + ".Params"
}

func validateEncodeBytes(params *types.Struct, fds *descriptor.FileDescriptorSet, msgName string) ([]byte, error) {
	if params == nil {
		return []byte{}, nil
	}
	d, err := toDictionary(params)
	if err != nil {
		return nil, fmt.Errorf("error converting parameters to dictionary: %v", err)
	}
	return yaml.NewEncoder(fds).EncodeBytes(d, msgName, false)
}

func (e *Ephemeral) processInstanceConfigs(errs *multierror.Error) (map[string]*InstanceStatic, int64) {
	instances := make(map[string]*InstanceStatic, len(e.templates))
	var instErrs int64

	for key, resource := range e.entries {
		var info *template.Info
		var found bool

		var params proto.Message
		instanceName := key.String()

		if key.Kind == constant.InstanceKind {
			inst := resource.Spec.(*config.Instance)
			if inst.CompiledTemplate == "" {
				continue
			}
			info, found = e.templates[inst.CompiledTemplate]
			if !found {
				instErrs++
				appendErr(errs, fmt.Sprintf("instance='%s'", instanceName), "missing compiled template")
				continue
			}

			// cast from struct to template specific proto
			params = proto.Clone(info.CtrCfg)
			buf := &bytes.Buffer{}

			if inst.Params == nil {
				inst.Params = &types.Struct{Fields: make(map[string]*types.Value)}
			}
			// populate attribute bindings
			if len(inst.AttributeBindings) > 0 {
				bindings := &types.Struct{Fields: make(map[string]*types.Value)}
				for k, v := range inst.AttributeBindings {
					bindings.Fields[k] = &types.Value{Kind: &types.Value_StringValue{StringValue: v}}
				}
				inst.Params.Fields["attribute_bindings"] = &types.Value{
					Kind: &types.Value_StructValue{StructValue: bindings},
				}
			}
			if err := (&jsonpb.Marshaler{}).Marshal(buf, inst.Params); err != nil {
				instErrs++
				appendErr(errs, fmt.Sprintf("instance='%s'", instanceName), err.Error())
				continue
			}
			if err := (&jsonpb.Unmarshaler{AllowUnknownFields: false}).Unmarshal(buf, params); err != nil {
				instErrs++
				appendErr(errs, fmt.Sprintf("instance='%s'", instanceName), err.Error())
				continue
			}
		} else {
			info, found = e.templates[key.Kind]
			if !found {
				// This config resource is not for an instance (or at least not for one that Mixer is currently aware of).
				continue
			}
			params = resource.Spec
		}

		mode := lang.GetLanguageRuntime(resource.Metadata.Annotations)

		log.Debugf("Processing incoming instance config: name='%s'\n%s", instanceName, params)
		inferredType, err := info.InferType(params, func(s string) (config.ValueType, error) {
			return e.checker(mode).EvalType(s)
		})

		if err != nil {
			instErrs++
			appendErr(errs, fmt.Sprintf("instance='%s'", instanceName), err.Error())
			continue
		}
		cfg := &InstanceStatic{
			Name:         instanceName,
			Template:     info,
			Params:       params,
			InferredType: inferredType,
			Language:     mode,
		}

		instances[cfg.Name] = cfg
	}

	return instances, instErrs
}

func (e *Ephemeral) processDynamicAdapterConfigs(ctx context.Context, availableTmpls map[string]*Template, errs *multierror.Error) map[string]*Adapter {
	result := map[string]*Adapter{}
	log.Debug("Begin processing adapter info configurations.")
	var adapterErrs int64
	for adapterInfoKey, resource := range e.entries {
		if adapterInfoKey.Kind != constant.AdapterKind {
			continue
		}

		adapterName := adapterInfoKey.String()

		cfg := resource.Spec.(*v1beta1.Info)

		log.Debugf("Processing incoming adapter info: name='%s'\n%v", adapterName, cfg)

		fds, desc, err := GetAdapterCfgDescriptor(cfg.Config)
		if err != nil {
			adapterErrs++
			appendErr(errs, fmt.Sprintf("adapter='%s'", adapterName), "unable to parse adapter configuration: %v", err)
			continue
		}
		supportedTmpls := make([]*Template, 0, len(cfg.Templates))
		for _, tmplN := range cfg.Templates {

			template, tmplFullName := getCanonicalRef(tmplN, constant.TemplateKind, adapterInfoKey.Namespace, func(n string) interface{} {
				if a, ok := availableTmpls[n]; ok {
					return a
				}
				return nil
			})
			if template == nil {
				adapterErrs++
				appendErr(errs, fmt.Sprintf("adapter='%s'", adapterName), "unable to find template '%s'", tmplN)
				continue
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

	stats.Record(ctx, monitoring.AdapterErrs.M(adapterErrs))
	return result
}

func assertType(tc lang.TypeChecker, expression string, expectedType config.ValueType) error {
	if t, err := tc.EvalType(expression); err != nil {
		return err
	} else if t != expectedType {
		return fmt.Errorf("expression '%s' evaluated to type %v, expected type %v", expression, t, expectedType)
	}
	return nil
}

func (e *Ephemeral) processRuleConfigs(
	ctx context.Context,
	sHandlers map[string]*HandlerStatic,
	sInstances map[string]*InstanceStatic,
	dHandlers map[string]*HandlerDynamic,
	dInstances map[string]*InstanceDynamic,
	errs *multierror.Error) []*Rule {

	log.Debug("Begin processing rule configurations.")
	var ruleErrs int64
	var rules []*Rule

	for ruleKey, resource := range e.entries {
		if ruleKey.Kind != constant.RulesKind {
			continue
		}

		ruleName := ruleKey.String()

		cfg := resource.Spec.(*config.Rule)
		mode := lang.GetLanguageRuntime(resource.Metadata.Annotations)

		log.Debugf("Processing incoming rule: name='%s'\n%s", ruleName, cfg)

		if cfg.Match != "" {
			if err := assertType(e.checker(mode), cfg.Match, config.BOOL); err != nil {
				ruleErrs++
				appendErr(errs, fmt.Sprintf("rule='%s'.Match", ruleName), err.Error())
			}
		}

		// extract the set of actions from the rule, and the handlers they reference.
		// A rule can have both static and dynamic actions.

		actionsStat := make([]*ActionStatic, 0, len(cfg.Actions))
		actionsDynamic := make([]*ActionDynamic, 0, len(cfg.Actions))
		for i, a := range cfg.Actions {
			log.Debugf("Processing action: %s[%d]", ruleName, i)
			var processStaticHandler bool
			var processDynamicHandler bool
			var sahandler *HandlerStatic
			var dahandler *HandlerDynamic
			hdl, handlerName := getCanonicalRef(a.Handler, constant.HandlerKind, ruleKey.Namespace, func(n string) interface{} {
				if a, ok := sHandlers[n]; ok {
					return a
				}
				return nil
			})
			if hdl != nil {
				sahandler = hdl.(*HandlerStatic)
				processStaticHandler = true
			} else {
				hdl, handlerName = getCanonicalRef(a.Handler, constant.HandlerKind, ruleKey.Namespace, func(n string) interface{} {
					if a, ok := dHandlers[n]; ok {
						return a
					}
					return nil
				})

				if hdl != nil {
					dahandler = hdl.(*HandlerDynamic)
					processDynamicHandler = true
				}
			}

			if !processStaticHandler && !processDynamicHandler {
				ruleErrs++
				appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i), "Handler not found: handler='%s'", a.Handler)
				continue
			}

			if processStaticHandler {
				// Keep track of unique instances, to avoid using the same instance multiple times within the same
				// action
				uniqueInstances := make(map[string]bool, len(a.Instances))

				actionInstances := make([]*InstanceStatic, 0, len(a.Instances))
				for _, instanceNameRef := range a.Instances {
					inst, instName := getCanonicalRef(instanceNameRef, constant.InstanceKind, ruleKey.Namespace, func(n string) interface{} {
						if a, ok := sInstances[n]; ok {
							return a
						}
						return nil
					})

					if inst == nil {
						ruleErrs++
						appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i), "Instance not found: instance='%s'", instanceNameRef)
						continue
					}

					instance := inst.(*InstanceStatic)

					if _, ok := uniqueInstances[instName]; ok {
						ruleErrs++
						appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i),
							"action specified the same instance multiple times: instance='%s',", instName)
						continue
					}
					uniqueInstances[instName] = true

					if !contains(sahandler.Adapter.SupportedTemplates, instance.Template.Name) {
						ruleErrs++
						appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i),
							"instance '%s' is of template '%s' which is not supported by handler '%s'",
							instName, instance.Template.Name, handlerName)
						continue
					}

					actionInstances = append(actionInstances, instance)
				}

				// If there are no valid instances found for this action, then elide the action.
				if len(actionInstances) == 0 {
					ruleErrs++
					appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i), "No valid instances found")
					continue
				}

				action := &ActionStatic{
					Handler:   sahandler,
					Instances: actionInstances,
					Name:      a.Name,
				}

				actionsStat = append(actionsStat, action)
			} else {
				// Keep track of unique instances, to avoid using the same instance multiple times within the same
				// action
				uniqueInstances := make(map[string]bool, len(a.Instances))

				actionInstances := make([]*InstanceDynamic, 0, len(a.Instances))
				for _, instanceNameRef := range a.Instances {
					inst, instName := getCanonicalRef(instanceNameRef, constant.InstanceKind, ruleKey.Namespace, func(n string) interface{} {
						if a, ok := dInstances[n]; ok {
							return a
						}
						return nil
					})

					if inst == nil {
						ruleErrs++
						appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i), "Instance not found: instance='%s'", instanceNameRef)
						continue
					}

					instance := inst.(*InstanceDynamic)
					if _, ok := uniqueInstances[instName]; ok {
						ruleErrs++
						appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i),
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
						ruleErrs++
						appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i),
							"instance '%s' is of template '%s' which is not supported by handler '%s'",
							instName, instance.Template.Name, handlerName)
						continue
					}

					actionInstances = append(actionInstances, instance)
				}

				// If there are no valid instances found for this action, then elide the action.
				if len(actionInstances) == 0 {
					ruleErrs++
					appendErr(errs, fmt.Sprintf("action='%s[%d]'", ruleName, i), "No valid instances found")
					continue
				}

				action := &ActionDynamic{
					Handler:   dahandler,
					Instances: actionInstances,
					Name:      a.Name,
				}

				actionsDynamic = append(actionsDynamic, action)
			}
		}

		// If there are no valid actions found for this rule, then elide the rule.
		if len(actionsStat) == 0 && len(actionsDynamic) == 0 &&
			len(cfg.RequestHeaderOperations) == 0 && len(cfg.ResponseHeaderOperations) == 0 {
			ruleErrs++
			appendErr(errs, fmt.Sprintf("rule=%s", ruleName), "No valid actions found in rule")
			continue
		}

		rule := &Rule{
			Name:                     ruleName,
			Namespace:                ruleKey.Namespace,
			ActionsStatic:            actionsStat,
			ActionsDynamic:           actionsDynamic,
			Match:                    cfg.Match,
			RequestHeaderOperations:  cfg.RequestHeaderOperations,
			ResponseHeaderOperations: cfg.ResponseHeaderOperations,
			Language:                 mode,
		}

		rules = append(rules, rule)
	}

	stats.Record(ctx, monitoring.RuleErrs.M(ruleErrs))

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

func (e *Ephemeral) processDynamicTemplateConfigs(ctx context.Context, errs *multierror.Error) map[string]*Template {
	result := map[string]*Template{}
	log.Debug("Begin processing templates.")
	var templateErrs int64
	for templateKey, resource := range e.entries {
		if templateKey.Kind != constant.TemplateKind {
			continue
		}

		templateName := templateKey.String()
		cfg := resource.Spec.(*v1beta1.Template)
		log.Debugf("Processing incoming template: name='%s'\n%v", templateName, cfg)

		fds, desc, name, variety, err := GetTmplDescriptor(cfg.Descriptor_)
		if err != nil {
			templateErrs++
			appendErr(errs, fmt.Sprintf("template='%s'", templateName), "unable to parse descriptor: %v", err)
			continue
		}

		result[templateName] = &Template{
			Name:                       templateName,
			InternalPackageDerivedName: name,
			FileDescSet:                fds,
			PackageName:                desc.GetPackage(),
			Variety:                    variety,
		}

		if variety == v1beta1.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR || variety == v1beta1.TEMPLATE_VARIETY_CHECK_WITH_OUTPUT {
			resolver := yaml.NewResolver(fds)
			msgName := "." + desc.GetPackage() + ".OutputMsg"
			// OutputMsg is a fixed generated name for the output template
			desc := resolver.ResolveMessage(msgName)
			if desc == nil {
				continue
			}

			// We only support a single level of nesting in output templates.
			attributes := make(map[string]*config.AttributeManifest_AttributeInfo)
			for _, field := range desc.GetField() {
				attributes["output."+field.GetName()] = &config.AttributeManifest_AttributeInfo{
					ValueType: yaml.DecodeType(resolver, field),
				}
			}

			result[templateName].AttributeManifest = attributes
		}
	}
	stats.Record(ctx, monitoring.TemplateErrs.M(templateErrs))
	return result
}

func appendErr(errs *multierror.Error, field string, format string, a ...interface{}) {
	err := fmt.Errorf(format, a...)
	log.Debug(err.Error())
	_ = multierror.Append(errs, adapter.ConfigError{Field: field, Underlying: err})
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
