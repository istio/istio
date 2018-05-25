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

package validator

import (
	"fmt"
	"strings"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	multierror "github.com/hashicorp/go-multierror"

	"istio.io/api/mixer/adapter/model/v1beta1"
	cpb "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/lang/ast"
	"istio.io/istio/mixer/pkg/lang/checker"
	"istio.io/istio/mixer/pkg/lang/compiled"
	"istio.io/istio/mixer/pkg/protobuf/yaml"
	"istio.io/istio/mixer/pkg/protobuf/yaml/dynamic"
	"istio.io/istio/mixer/pkg/runtime/config"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/istio/pkg/cache"
	"istio.io/istio/pkg/log"
)

// Validator offers semantic validation of the config changes.
type Validator struct {
	handlerBuilders map[string]adapter.HandlerBuilder
	templates       map[string]*template.Info
	tc              checker.TypeChecker
	af              ast.AttributeDescriptorFinder
	c               *validatorCache
	donec           chan struct{}
}

// New creates a new store.Validator instance which validates runtime semantics of
// the configs.
func New(tc checker.TypeChecker, identityAttribute string, s store.Store,
	adapterInfo map[string]*adapter.Info, templateInfo map[string]*template.Info) (store.Validator, error) {
	kinds := config.KindMap(adapterInfo, templateInfo)
	data, ch, err := store.StartWatch(s, kinds)
	if err != nil {
		return nil, err
	}
	hb := make(map[string]adapter.HandlerBuilder, len(adapterInfo))
	for k, ai := range adapterInfo {
		hb[k] = ai.NewBuilder()
	}
	configData := make(map[store.Key]proto.Message, len(data))
	manifests := map[store.Key]*cpb.AttributeManifest{}
	for k, obj := range data {
		if k.Kind == config.AttributeManifestKind {
			manifests[k] = obj.Spec.(*cpb.AttributeManifest)
		}
		configData[k] = obj.Spec
	}
	v := &Validator{
		handlerBuilders: hb,
		templates:       templateInfo,
		tc:              tc,
		c: &validatorCache{
			c:          cache.NewTTL(validatedDataExpiration, validatedDataEviction),
			configData: configData,
		},
		donec: make(chan struct{}),
	}
	go store.WatchChanges(ch, v.donec, time.Second, v.c.applyChanges)
	v.af = v.newAttributeDescriptorFinder(manifests)
	return v, nil
}

// Stop stops the validator.
func (v *Validator) Stop() {
	close(v.donec)
}

func (v *Validator) refreshTypeChecker() {
	manifests := map[store.Key]*cpb.AttributeManifest{}
	v.c.forEach(func(key store.Key, spec proto.Message) {
		if key.Kind == config.AttributeManifestKind {
			manifests[key] = spec.(*cpb.AttributeManifest)
		}
	})
	v.af = v.newAttributeDescriptorFinder(manifests)
}

func (v *Validator) formKey(value, desiredKind, namespace string) (store.Key, error) {

	// In new style config the kind is implied and *must* not be specified in the reference.
	// only allowed references are <name> or <name>.<namespace>
	parts := strings.Split(value, ".")
	switch len(parts) {
	case 1:
		return store.Key{
			Name:      parts[0],
			Kind:      desiredKind,
			Namespace: namespace,
		}, nil
	case 2:
		// this case is ambiguous between old config model and new; we don't know if 2nd part
		// is the kind or the namespace.
		// First try old style config where 2nd part is the kind.
		k := parts[1]
		if desiredKind == config.InstanceKind {
			// check if the k (2nd part) is one of the mixer's built in templates; if true then config is old style
			if _, ok := v.templates[k]; ok {
				return store.Key{
					Name:      parts[0],
					Kind:      parts[1],
					Namespace: namespace,
				}, nil
			}
		}
		if desiredKind == config.HandlerKind {
			// check if the k (2nd part) is one of the mixer's built in adapters; if true then config is old style
			if _, ok := v.handlerBuilders[k]; ok {
				return store.Key{
					Name:      parts[0],
					Kind:      parts[1],
					Namespace: namespace,
				}, nil
			}
		}
		// else it is potentially a new style config where second part is the namespace and kind is implied
		return store.Key{
			Name:      parts[0],
			Kind:      desiredKind,
			Namespace: parts[1],
		}, nil
	case 3:
		return store.Key{
			Name:      parts[0],
			Kind:      parts[1],
			Namespace: parts[2],
		}, nil
	default:
		return store.Key{}, fmt.Errorf("illformed %s", value)
	}
}

func (v *Validator) newAttributeDescriptorFinder(manifests map[store.Key]*cpb.AttributeManifest) ast.AttributeDescriptorFinder {
	attrs := map[string]*cpb.AttributeManifest_AttributeInfo{}
	for _, manifest := range manifests {
		for an, at := range manifest.Attributes {
			attrs[an] = at
		}
	}
	return ast.NewFinder(attrs)
}

func (v *Validator) validateUpdateRule(namespace string, rule *cpb.Rule) error {
	var errs error
	if rule.Match != "" {
		if err := v.tc.AssertType(rule.Match, v.af, cpb.BOOL); err != nil {
			errs = multierror.Append(errs, &adapter.ConfigError{Field: "match", Underlying: err})
		}
	}
	for i, action := range rule.Actions {
		key, err := v.formKey(action.Handler, config.HandlerKind, namespace)
		if err != nil {
			errs = multierror.Append(errs, &adapter.ConfigError{
				Field:      fmt.Sprintf("actions[%d].handler", i),
				Underlying: err,
			})
			continue
		}
		if _, ok := v.handlerBuilders[key.Kind]; ok && key.Kind != "handler" {
			if _, ok = v.c.get(key); !ok {
				err = fmt.Errorf("%s not found", action.Handler)
			}
			if err != nil {
				errs = multierror.Append(errs, &adapter.ConfigError{
					Field:      fmt.Sprintf("actions[%d].handler", i),
					Underlying: err,
				})
			}
			for j, instance := range action.Instances {
				key, err = v.formKey(instance, config.InstanceKind, namespace)
				if err == nil {
					if _, ok := v.templates[key.Kind]; ok {
						if _, ok = v.c.get(key); !ok {
							err = fmt.Errorf("%s not found", instance)
						}
					} else {
						err = fmt.Errorf("%s is not an instance", key.Kind)
					}
				}
				if err != nil {
					errs = multierror.Append(errs, &adapter.ConfigError{
						Field:      fmt.Sprintf("actions[%d].instances[%d]", i, j),
						Underlying: err,
					})
				}
			}
		} else {
			aKey, info, err := v.getAdapterFromHandler(action.Handler, namespace)
			if err != nil {
				errs = multierror.Append(errs, &adapter.ConfigError{
					Field:      fmt.Sprintf("actions[%d].handler '%s'", i, action.Handler),
					Underlying: err,
				})
				continue
			}

			// ignore the error, since during rule validation we don't care of the adapter is broken.
			// Ideally this will not happen as the adapter validation should have caught it.
			supportedTmpls, _ := v.getFQns(info.Templates, config.TemplateKind, namespace)
			for j, instance := range action.Instances {
				_, inst, err := v.getInstance(instance, namespace)
				if err != nil {
					errs = multierror.Append(errs, &adapter.ConfigError{
						Field:      fmt.Sprintf("actions[%d].instances[%d] '%s'", i, j, instance),
						Underlying: err,
					})
					continue
				}

				// ignore the error, since during rule validation we don't care of the instance is broken.
				// Ideally this will not happen as the instance validation should have caught it.
				tKey, _ := v.formKey(inst.Template, config.TemplateKind, namespace)
				if !contains(supportedTmpls, tKey.String()) {
					errs = multierror.Append(errs, &adapter.ConfigError{
						Field: fmt.Sprintf("actions[%d].instances[%d].Template '%s'", i, j, inst.Template),
						Underlying: fmt.Errorf("actions[%d].handler.adapter '%s' does not "+
							"support template '%s'. Only supported templates are %s", i,
							aKey, inst.Template, strings.Join(info.Templates, ",")),
					})
				}
			}
		}
	}
	return errs
}

func (v *Validator) getFQns(refs []string, kind, ns string) ([]string, error) {
	fqns := make([]string, 0, len(refs))
	var errs error

	for _, s := range refs {
		key, err := v.formKey(s, kind, ns)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}
		fqns = append(fqns, key.String())
	}

	return fqns, errs
}

func (v *Validator) validateManifests(af ast.AttributeDescriptorFinder) error {
	var errs error
	v.c.forEach(func(key store.Key, spec proto.Message) {
		var err error
		if ti, ok := v.templates[key.Kind]; ok {
			_, err = ti.InferType(spec, func(s string) (cpb.ValueType, error) {
				return v.tc.EvalType(s, af)
			})
		} else if key.Kind == config.RulesKind {
			rule := spec.(*cpb.Rule)
			if rule.Match != "" {
				if aerr := v.tc.AssertType(rule.Match, v.af, cpb.BOOL); aerr != nil {
					err = &adapter.ConfigError{Field: "match", Underlying: aerr}
				}
			}
		}
		if err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failure on %s with the new manifest: %v", key, err))
		}
	})
	return errs
}

func (v *Validator) validateDelete(key store.Key) error {
	if _, ok := v.handlerBuilders[key.Kind]; ok {
		if err := v.validateHandlerDelete(key); err != nil {
			return err
		}
	} else if _, ok = v.templates[key.Kind]; ok {
		if err := v.validateInstanceDelete(key); err != nil {
			return err
		}
	} else if key.Kind == config.AttributeManifestKind {
		manifests := map[store.Key]*cpb.AttributeManifest{}
		v.c.forEach(func(k store.Key, spec proto.Message) {
			if k.Kind == config.AttributeManifestKind && k != key {
				manifests[k] = spec.(*cpb.AttributeManifest)
			}
		})
		af := v.newAttributeDescriptorFinder(manifests)
		if err := v.validateManifests(af); err != nil {
			return err
		}
		v.af = af
		go func() {
			<-time.After(validatedDataExpiration)
			v.refreshTypeChecker()
		}()
	} else if key.Kind == config.TemplateKind {
		if err := v.validateTemplateDelete(key); err != nil {
			return err
		}
	} else if key.Kind == config.AdapterKind {
		if err := v.validateAdapterDelete(key); err != nil {
			return err
		}
	} else if key.Kind == config.HandlerKind {
		if err := v.validateHandlerDelete(key); err != nil {
			return err
		}
	} else if key.Kind == config.InstanceKind {
		if err := v.validateInstanceDelete(key); err != nil {
			return err
		}
	} else {
		log.Debugf("don't know how to validate %s", key)
	}
	return nil
}

func (v *Validator) validateUpdate(ev *store.Event) error {
	if hb, ok := v.handlerBuilders[ev.Kind]; ok {
		// found a compiled in adapter
		hb.SetAdapterConfig((adapter.Config)(ev.Value.Spec))
		if err := hb.Validate(); err != nil {
			return err
		}
	} else if ti, ok := v.templates[ev.Kind]; ok {
		_, err := ti.InferType(ev.Value.Spec, func(s string) (cpb.ValueType, error) {
			return v.tc.EvalType(s, v.af)
		})
		if err != nil {
			return err
		}
	} else if rule, ok := ev.Value.Spec.(*cpb.Rule); ok && ev.Kind == config.RulesKind {
		if err := v.validateUpdateRule(ev.Namespace, rule); err != nil {
			return err
		}
	} else if manifest, ok := ev.Value.Spec.(*cpb.AttributeManifest); ok && ev.Kind == config.AttributeManifestKind {
		manifests := map[store.Key]*cpb.AttributeManifest{}
		v.c.forEach(func(k store.Key, spec proto.Message) {
			if k.Kind == config.AttributeManifestKind {
				manifests[k] = spec.(*cpb.AttributeManifest)
			}
		})
		manifests[ev.Key] = manifest
		af := v.newAttributeDescriptorFinder(manifests)
		if err := v.validateManifests(af); err != nil {
			return err
		}
		v.af = af
		go func() {
			<-time.After(validatedDataExpiration)
			v.refreshTypeChecker()
		}()
	} else if adptInfo, ok := ev.Value.Spec.(*v1beta1.Info); ok && ev.Kind == config.AdapterKind {
		if err := v.validateUpdateAdapter(ev.Key, ev.Namespace, adptInfo); err != nil {
			return err
		}
	} else if tmpl, ok := ev.Value.Spec.(*v1beta1.Template); ok && ev.Kind == config.TemplateKind {
		if err := v.validateUpdateTemplate(ev.Key, ev.Namespace, tmpl); err != nil {
			return err
		}
	} else if hdl, ok := ev.Value.Spec.(*cpb.Handler); ok && ev.Kind == config.HandlerKind {
		if err := v.validateUpdateHandler(ev.Key, ev.Namespace, hdl); err != nil {
			return err
		}
	} else if inst, ok := ev.Value.Spec.(*cpb.Instance); ok && ev.Kind == config.InstanceKind {
		if err := v.validateUpdateInstance(ev.Key, ev.Namespace, inst); err != nil {
			return err
		}
	} else {
		log.Debugf("don't know how to validate %s", ev.Key)
	}
	return nil
}

func (v *Validator) validateUpdateTemplate(tKey store.Key, namespace string, tmpl *v1beta1.Template) error {
	var fds *descriptor.FileDescriptorSet
	var fdp *descriptor.FileDescriptorProto
	var err error
	if fds, fdp, _, err = config.GetTmplDescriptor(tmpl.GetDescriptor_()); err != nil {
		return adapter.ConfigError{
			Field:      fmt.Sprintf("template[%s].descriptor", tKey),
			Underlying: err,
		}
	}

	// validate if instance's param is still valid upon template update
	instances := v.getInstances(tKey.String(), namespace)
	compiler := compiled.NewBuilder(v.af)
	resolver := yaml.NewResolver(fds)
	b := dynamic.NewEncoderBuilder(
		resolver,
		compiler,
		false)
	for iKey, v := range instances {
		if v.Params != nil {
			if _, err = b.Build(getTemplatesMsgFullName(fdp), v.Params.(map[string]interface{})); err != nil {
				return adapter.ConfigError{
					Field:      fmt.Sprintf("instance[%s].Params", iKey),
					Underlying: err,
				}
			}
		}
	}

	return nil
}

func (v *Validator) validateTemplateDelete(ikey store.Key) error {
	var errs error
	v.c.forEach(func(key store.Key, spec proto.Message) {
		if key.Kind == config.AdapterKind {
			info := spec.(*v1beta1.Info)
			for i, tmplRef := range info.Templates {
				tKey, err := v.formKey(tmplRef, config.TemplateKind, key.Namespace)
				if err != nil {
					// invalid rules are already in the cache; simply log it and continue
					log.Errorf("invalid template name %s in adapter %s", tmplRef, key)
					continue
				}
				if tKey == ikey {
					errs = multierror.Append(errs, &adapter.ConfigError{Field: fmt.Sprintf("adapter[%s]/templates[%d]", key, i),
						Underlying: fmt.Errorf("references to be deleted template %s", ikey)})
				}
			}

		}

		if key.Kind == config.InstanceKind {
			instance := spec.(*cpb.Instance)
			iKey, err := v.formKey(instance.Template, config.TemplateKind, key.Namespace)
			if err != nil {
				// invalid rules are already in the cache; simply log it and continue
				log.Errorf("invalid template name %s in instance %s", instance.Template, key)
			}
			if iKey == ikey {
				errs = multierror.Append(errs, &adapter.ConfigError{Field: fmt.Sprintf("instance[%s].template", key),
					Underlying: fmt.Errorf("references to be deleted template %s", ikey)})
			}
		}
	})
	return errs
}

func (v *Validator) validateUpdateAdapter(adptKey store.Key, namespace string, info *v1beta1.Info) error {
	var errs error

	// Check descriptor string is correct
	var fds *descriptor.FileDescriptorSet
	var fdp *descriptor.FileDescriptorProto
	var err error
	if fds, fdp, err = config.GetAdapterCfgDescriptor(info.Config); err != nil {
		errs = multierror.Append(errs, &adapter.ConfigError{Field: fmt.Sprintf("adapter[%s].config", adptKey),
			Underlying: err})
	}

	// Check referenced templates are correct
	var supportedTmpls []string
	if supportedTmpls, err = v.validateAdapterTemplateRef(info, namespace); err != nil {
		errs = multierror.Append(errs, err)
		return errs
	}

	// Change in adapter info can affect the descriptor and/or the
	// supported templates, resulting into breaking the handler and/or the instances routed to the adapter.
	// We need to validate both
	refHandlers := v.getHandlers(adptKey, namespace)
	for hkey, hdl := range refHandlers {
		// Check param is not broken
		if err := validateEncodeBytes(hdl.Params, fds, getParamsMsgFullName(fdp)); err != nil {
			errs = multierror.Append(errs, &adapter.ConfigError{Field: fmt.Sprintf("handler[%s].adapter", hkey),
				Underlying: err})
		}

		// Check routed instances are still supported
		refAction := v.getActionsFromHandler(hkey.String(), namespace)
		for id, action := range refAction {
			if err := v.validateRoutedInstancesAreSupported(action, namespace, supportedTmpls,
				adptKey.String()); err != nil {
				errs = multierror.Append(errs,
					&adapter.ConfigError{Field: fmt.Sprintf("%s.instances", id),
						Underlying: err},
				)
			}
		}
	}

	return errs
}

func (v *Validator) validateAdapterDelete(aKey store.Key) error {
	var errs error
	v.c.forEach(func(hKey store.Key, spec proto.Message) {
		if hKey.Kind == config.HandlerKind {
			hdl := spec.(*cpb.Handler)
			k, err := v.formKey(hdl.Adapter, config.AdapterKind, hKey.Namespace)
			if err != nil {
				// invalid handler is already in the cache; simply log it and continue
				log.Errorf("invalid adapter name %s in handler %s", hdl.Adapter, hKey)
			}
			if k == aKey {
				errs = multierror.Append(errs, &adapter.ConfigError{Field: fmt.Sprintf("handler[%s].adapter", hKey),
					Underlying: fmt.Errorf("references to be deleted adapter %s", aKey)})
			}
		}
	})
	return errs
}

func (v *Validator) validateUpdateInstance(iKey store.Key, ns string, instance *cpb.Instance) error {
	var errs error

	// make sure params conforms to templates's descriptor.
	err := v.validateInstanceTemplateRef(instance, ns)
	if err != nil {
		errs = multierror.Append(errs, &adapter.ConfigError{
			Field:      fmt.Sprintf("instance[%s]", iKey),
			Underlying: err,
		})
	}

	// make sure adapter to which the updated instance is dispatched supports the template of the instance.
	actions := v.getActionsFromInstances(iKey, ns)
	for id, action := range actions {
		if akey, adpt, err := v.getAdapterFromHandler(action.Handler, ns); err != nil {
			errs = multierror.Append(errs, &adapter.ConfigError{
				Field:      fmt.Sprintf("%s.handler", id),
				Underlying: err,
			})
		} else {
			if !contains(adpt.Templates, instance.Template) {
				errs = multierror.Append(errs, &adapter.ConfigError{
					Field: fmt.Sprintf("%s.handler.adapter '%s'", id, akey),
					Underlying: fmt.Errorf("instance[%s].Template '%s' is not supported by adapter of the "+
						"handler to which the instance is routed", iKey, instance.Template),
				})
			}
		}
	}
	return errs
}

func (v *Validator) validateInstanceDelete(ikey store.Key) error {
	var errs error
	v.c.forEach(func(rkey store.Key, spec proto.Message) {
		if rkey.Kind != config.RulesKind {
			return
		}
		rule := spec.(*cpb.Rule)
		for i, action := range rule.Actions {
			for j, instance := range action.Instances {
				key, err := v.formKey(instance, config.InstanceKind, rkey.Namespace)
				if err != nil {
					// invalid rules are already in the cache; simply log it and continue
					log.Errorf("Invalid handler value %s in %s", instance, rkey)
					continue
				}
				if key == ikey {
					errs = multierror.Append(errs, fmt.Errorf("%s is referred by %s/actions[%d].instances[%d]",
						ikey, rkey, i, j))
				}
			}
		}
	})
	return errs
}

func (v *Validator) validateUpdateHandler(hkey store.Key, namespace string, handler *cpb.Handler) error {
	var errs error

	// make sure params conforms to adapter's descriptor.
	adapterKey, info, err := v.validateHandlerAdapterRef(handler, namespace)
	if err != nil {
		errs = multierror.Append(errs, &adapter.ConfigError{
			Field:      fmt.Sprintf("handler[%s]", hkey),
			Underlying: err,
		})
		return errs
	}

	// make sure existing rules are not broken in case the handler points to a new/incompatible adapter.
	refAction := v.getActionsFromHandler(hkey.String(), namespace)
	// ignore the error, since during handler validation we don't care of the adapter is broken.
	// Ideally this will not happen as the adapter validation should have caught it.
	supportedTmpls, _ := v.getFQns(info.Templates, config.TemplateKind, namespace)
	for id, action := range refAction {
		if err = v.validateRoutedInstancesAreSupported(action, namespace, supportedTmpls, adapterKey.String()); err != nil {
			errs = multierror.Append(errs,
				&adapter.ConfigError{
					Field:      fmt.Sprintf("%s.instances", id),
					Underlying: err,
				})
		}
	}

	// TODO validate connections
	return errs
}

func (v *Validator) validateHandlerDelete(hkey store.Key) error {
	var errs error
	v.c.forEach(func(rkey store.Key, spec proto.Message) {
		if rkey.Kind != config.RulesKind {
			return
		}
		rule := spec.(*cpb.Rule)
		for i, action := range rule.Actions {
			key, err := v.formKey(action.Handler, config.HandlerKind, rkey.Namespace)
			if err != nil {
				// invalid rules are already in the cache; simply log it and continue
				log.Errorf("Invalid handler value %s in %s", action.Handler, rkey)
				continue
			}
			if err != nil {
				// invalid rules are already in the cache; simply log it and continue
				log.Errorf("Invalid handler value %s in %s", action.Handler, rkey)
				continue
			}
			if key == hkey {
				errs = multierror.Append(errs, fmt.Errorf("%s is referred by %s/actions[%d].handler", hkey, rkey, i))
			}
		}
	})
	return errs
}

// Validate implements store.Validator interface.
func (v *Validator) Validate(ev *store.Event) error {
	var err error
	if ev.Type == store.Delete {
		err = v.validateDelete(ev.Key)
	} else {
		err = v.validateUpdate(ev)
	}
	if err == nil {
		v.c.putCache(ev)
	}
	return err
}

func (v *Validator) validateAdapterTemplateRef(adptInfo *v1beta1.Info, namespace string) ([]string, error) {

	var errs error
	tmplsFQNs := make([]string, 0)
	for i, tmpl := range adptInfo.Templates {
		key, err := v.formKey(tmpl, config.TemplateKind, namespace)
		if err == nil {
			if key.Kind != config.TemplateKind {
				err = fmt.Errorf("%s is not a template", tmpl)
			} else if _, ok := v.c.get(key); !ok {
				err = fmt.Errorf("%s not found", tmpl)
			} else {
				tmplsFQNs = append(tmplsFQNs, key.String())
			}
		}

		if err != nil {
			errs = multierror.Append(errs, &adapter.ConfigError{
				Field:      fmt.Sprintf("templates[%d]", i),
				Underlying: err,
			})
		}
	}
	return tmplsFQNs, errs
}

func (v *Validator) validateHandlerAdapterRef(handler *cpb.Handler, namespace string) (store.Key, *v1beta1.Info, error) {
	// TODO. NOTE: adapter name must be a fqn. ??
	adapterKey, err := v.formKey(handler.Adapter, config.AdapterKind, namespace)
	var info *v1beta1.Info
	if err == nil {
		if adapterKey.Kind != config.AdapterKind {
			err = fmt.Errorf("%s is not a adapter", handler.Adapter)
		} else if info2, ok := v.c.get(adapterKey); !ok {
			err = fmt.Errorf("%s not found", handler.Adapter)
		} else {
			var desc *descriptor.FileDescriptorProto
			var fds *descriptor.FileDescriptorSet
			info = info2.(*v1beta1.Info)
			fds, desc, err = config.GetAdapterCfgDescriptor(info.Config)
			if err == nil {
				err = validateEncodeBytes(handler.Params, fds, getParamsMsgFullName(desc))
			}
		}
	}
	return adapterKey, info, err
}

func (v *Validator) validateInstanceTemplateRef(instance *cpb.Instance, namespace string) error {
	_, template, err := v.getTemplate(instance.Template, namespace)
	if err == nil {
		if instance.Params != nil {
			var fdp *descriptor.FileDescriptorProto
			var fds *descriptor.FileDescriptorSet
			fds, fdp, _, err = config.GetTmplDescriptor(template.GetDescriptor_())

			if err == nil {
				// bad template is already in the store.
				compiler := compiled.NewBuilder(v.af)
				resolver := yaml.NewResolver(fds)
				b := dynamic.NewEncoderBuilder(
					resolver,
					compiler,
					false)
				_, err = b.Build(getTemplatesMsgFullName(fdp), instance.Params.(map[string]interface{}))
			}
		}

	}
	return err
}

func (v *Validator) validateRoutedInstancesAreSupported(action *cpb.Action, namespace string, supportedTmpls []string,
	adptName string) error {
	var errs error

	for _, iName := range action.Instances {
		instKey, instance, err := v.getInstance(iName, namespace)
		if err != nil {
			// invalid rule is already in the cache; simply log it and continue
			log.Errorf("error with instance %s: %s", instKey, err.Error())
			continue
		}

		tmplKey, _, err := v.getTemplate(instance.Template, namespace)
		if err != nil {
			// invalid instance is already in the cache; simply log it and continue
			log.Errorf("error with template %s in instance %s: %s", tmplKey, instKey, err.Error())
			continue
		}

		// validate if the handler accepts the templates of the instances passed to it.
		if !contains(supportedTmpls, tmplKey.String()) {
			errs = multierror.Append(errs, fmt.Errorf("adapter %s does not support template %s of the instance %s", adptName, tmplKey, instKey))
		}
	}

	return errs
}

func (v *Validator) getTemplate(tKey string, ns string) (store.Key, *v1beta1.Template, error) {
	tmplKey, err := v.formKey(tKey, config.TemplateKind, ns)
	if err != nil {
		// invalid template entry are already in the cache; simply log it and continue
		return store.Key{}, nil, fmt.Errorf("invalid template value %s", tKey)
	}
	var ok bool
	var template proto.Message
	if template, ok = v.c.get(tmplKey); !ok || tmplKey.Kind != config.TemplateKind {
		return store.Key{}, nil, fmt.Errorf("template %s not found ", tmplKey)
	}
	return tmplKey, template.(*v1beta1.Template), err
}

func (v *Validator) getInstance(iKey string, ns string) (store.Key, *cpb.Instance, error) {
	instKey, err := v.formKey(iKey, config.InstanceKind, ns)
	if err != nil {
		// invalid instance entry are already in the cache; simply log it and continue
		return store.Key{}, nil, fmt.Errorf("invalid instance value %s", iKey)
	}
	var ok bool
	var instance proto.Message
	if instance, ok = v.c.get(instKey); !ok || instKey.Kind != config.InstanceKind {
		return store.Key{}, nil, fmt.Errorf("instance %s not found ", instKey)
	}
	return instKey, instance.(*cpb.Instance), err
}

func (v *Validator) getHandlers(aKey store.Key, namespace string) map[store.Key]*cpb.Handler {
	refHandlers := make(map[store.Key]*cpb.Handler)
	v.c.forEach(func(hKey store.Key, spec proto.Message) {
		if hKey.Kind != config.HandlerKind {
			return
		}
		handler := spec.(*cpb.Handler)
		aKeyRef, _ := v.formKey(handler.Adapter, config.AdapterKind, namespace)
		if aKeyRef != aKey {
			return
		}

		refHandlers[hKey] = handler
	})

	return refHandlers
}

func (v *Validator) getAdapterFromHandler(hRef string, ns string) (store.Key, *v1beta1.Info, error) {
	hdlKey, err := v.formKey(hRef, config.HandlerKind, ns)
	if err != nil {
		// invalid handler entry are already in the cache
		return store.Key{}, nil, fmt.Errorf("%s is not a valid handler: %v", hRef, err)
	}
	var ok bool
	var hdl proto.Message
	if hdl, ok = v.c.get(hdlKey); !ok || hdlKey.Kind != config.HandlerKind {
		return store.Key{}, nil, fmt.Errorf("handler %s not found ", hdlKey)
	}

	aRef := hdl.(*cpb.Handler).Adapter
	adptKey, err := v.formKey(aRef, config.AdapterKind, ns)
	if err != nil {
		// invalid handler entry are already in the cache
		return store.Key{}, nil, fmt.Errorf("handler[%s].adapter %s is not a valid adapter: %v", hdlKey, aRef, err)
	}
	var adpt proto.Message
	if adpt, ok = v.c.get(adptKey); !ok || adptKey.Kind != config.AdapterKind {
		return store.Key{}, nil, fmt.Errorf("handler[%s].adapter %s not found ", hdlKey, adptKey)
	}

	return adptKey, adpt.(*v1beta1.Info), err
}

func (v *Validator) getInstances(tmplKeyStr string, namespace string) map[store.Key]*cpb.Instance {
	refInstances := make(map[store.Key]*cpb.Instance)
	v.c.forEach(func(iKey store.Key, spec proto.Message) {
		if iKey.Kind != config.InstanceKind {
			return
		}
		inst := spec.(*cpb.Instance)
		tmplRef, err := v.formKey(inst.Template, config.TemplateKind, namespace)
		if err != nil {
			return
		}
		if tmplRef.String() != tmplKeyStr {
			return
		}

		refInstances[iKey] = inst
	})

	return refInstances
}

func (v *Validator) getActionsFromHandler(hKeyStr, namespace string) map[string]*cpb.Action {
	refAction := make(map[string]*cpb.Action)
	v.c.forEach(func(rkey store.Key, spec proto.Message) {
		if rkey.Kind != config.RulesKind {
			return
		}
		rule := spec.(*cpb.Rule)
		for i, action := range rule.Actions {
			hRef, _ := v.formKey(action.Handler, config.HandlerKind, namespace)
			if hRef.String() != hKeyStr {
				continue
			}
			refAction[fmt.Sprintf("rule[%s].actions[%d]", rkey, i)] = action
		}
	})
	return refAction
}

func (v *Validator) getActionsFromInstances(iKey store.Key, namespace string) map[string]*cpb.Action {
	refAction := make(map[string]*cpb.Action)
	v.c.forEach(func(rkey store.Key, spec proto.Message) {
		if rkey.Kind != config.RulesKind {
			return
		}
		rule := spec.(*cpb.Rule)
		for i, action := range rule.Actions {
			instances, err := v.getFQns(action.Instances, config.InstanceKind, namespace)
			if err != nil {
				continue
			}
			if !contains(instances, iKey.String()) {
				continue
			}
			refAction[fmt.Sprintf("rule[%s].actions[%d]", rkey, i)] = action
		}
	})
	return refAction
}

func validateEncodeBytes(params interface{}, fds *descriptor.FileDescriptorSet, msgName string) error {
	// TODO undo this waste translation after PR https://github.com/istio/istio/pull/5277 is submitted
	tmpParams := make(map[interface{}]interface{})
	if params != nil {
		for k, v := range params.(map[string]interface{}) {
			tmpParams[k] = v
		}
	}
	_, err := yaml.NewEncoder(fds).EncodeBytes(tmpParams, msgName, false)
	return err
}

func getParamsMsgFullName(desc *descriptor.FileDescriptorProto) string {
	return "." + desc.GetPackage() + ".Params"
}

func getTemplatesMsgFullName(desc *descriptor.FileDescriptorProto) string {
	return "." + desc.GetPackage() + ".Template"
}

func contains(set []string, entry string) bool {
	for _, s := range set {
		if entry == s {
			return true
		}
	}
	return false
}
