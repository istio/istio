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
	multierror "github.com/hashicorp/go-multierror"

	cpb "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/expr"
	"istio.io/istio/mixer/pkg/il/evaluator"
	"istio.io/istio/mixer/pkg/runtime2/config"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/istio/pkg/cache"
	"istio.io/istio/pkg/log"
)

// Validator offers semantic validation of the config changes.
type Validator struct {
	handlerBuilders map[string]adapter.HandlerBuilder
	templates       map[string]*template.Info
	tc              *evaluator.IL
	af              expr.AttributeDescriptorFinder
	c               *validatorCache
	donec           chan struct{}
}

// New creates a new store.Validator instance which validates runtime semantics of
// the configs.
func New(tc *evaluator.IL, identityAttribute string, s store.Store,
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
	v.tc.ChangeVocabulary(v.af)
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
	v.tc.ChangeVocabulary(v.af)
}

func (v *Validator) getKey(value, namespace string) (store.Key, error) {
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return store.Key{}, fmt.Errorf("illformed %s", value)
	}
	key := store.Key{
		Kind: parts[1],
		Name: parts[0],
	}
	if len(parts) == 2 {
		key.Namespace = namespace
	} else if len(parts) == 3 {
		key.Namespace = parts[2]
	} else {
		return store.Key{}, fmt.Errorf("illformed %s, too many parts", value)
	}
	return key, nil
}

func (v *Validator) newAttributeDescriptorFinder(manifests map[store.Key]*cpb.AttributeManifest) expr.AttributeDescriptorFinder {
	attrs := map[string]*cpb.AttributeManifest_AttributeInfo{}
	for _, manifest := range manifests {
		for an, at := range manifest.Attributes {
			attrs[an] = at
		}
	}
	return expr.NewFinder(attrs)
}

func (v *Validator) validateUpdateRule(namespace string, rule *cpb.Rule) error {
	var errs error
	if rule.Match != "" {
		if err := v.tc.AssertType(rule.Match, v.af, cpb.BOOL); err != nil {
			errs = multierror.Append(errs, &adapter.ConfigError{Field: "match", Underlying: err})
		}
	}
	for i, action := range rule.Actions {
		key, err := v.getKey(action.Handler, namespace)
		if err == nil {
			if _, ok := v.handlerBuilders[key.Kind]; ok {
				if _, ok = v.c.get(key); !ok {
					err = fmt.Errorf("%s not found", action.Handler)
				}
			} else {
				err = fmt.Errorf("%s is not a handler", key.Kind)

			}
		}
		if err != nil {
			errs = multierror.Append(errs, &adapter.ConfigError{
				Field:      fmt.Sprintf("actions[%d].handler", i),
				Underlying: err,
			})
		}
		for j, instance := range action.Instances {
			key, err = v.getKey(instance, namespace)
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
	}
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
			key, err := v.getKey(action.Handler, rkey.Namespace)
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

func (v *Validator) validateInstanceDelete(ikey store.Key) error {
	var errs error
	v.c.forEach(func(rkey store.Key, spec proto.Message) {
		if rkey.Kind != config.RulesKind {
			return
		}
		rule := spec.(*cpb.Rule)
		for i, action := range rule.Actions {
			for j, instance := range action.Instances {
				key, err := v.getKey(instance, rkey.Namespace)
				if err != nil {
					// invalid rules are already in the cache; simply log it and continue
					log.Errorf("Invalid handler value %s in %s", instance, rkey)
					continue
				}
				if key == ikey {
					errs = multierror.Append(errs, fmt.Errorf("%s is referred by %s/actions[%d].instances[%d]", ikey, rkey, i, j))
				}
			}
		}
	})
	return errs
}

func (v *Validator) validateManifests(af expr.AttributeDescriptorFinder) error {
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
		v.tc.ChangeVocabulary(af)
		if err := v.validateManifests(af); err != nil {
			return err
		}
		v.af = af
		go func() {
			<-time.After(validatedDataExpiration)
			v.refreshTypeChecker()
		}()
	} else {
		log.Debugf("don't know how to validate %s", key)
	}
	return nil
}

func (v *Validator) validateUpdate(ev *store.Event) error {
	if hb, ok := v.handlerBuilders[ev.Kind]; ok {
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
		v.tc.ChangeVocabulary(af)
		if err := v.validateManifests(af); err != nil {
			return err
		}
		v.af = af
		go func() {
			<-time.After(validatedDataExpiration)
			v.refreshTypeChecker()
		}()
	} else {
		log.Debugf("don't know how to validate %s", ev.Key)
	}
	return nil
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
