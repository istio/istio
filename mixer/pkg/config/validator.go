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

// Package config handles configuration ingestion and processing.
// validator
// 1. Accepts new configuration from user
// 2. Validates configuration
// 3. Produces a "ValidatedConfig"
// runtime
// 1. It is validated and actionable configuration
// 2. It resolves the configuration to a list of Combined {aspect, adapter} configs
//    given an attribute.Bag.
// 3. Combined config has complete information needed to dispatch aspect
package config

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/config/descriptor"
	pb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
)

type (
	// AspectParams describes configuration parameters for an aspect.
	AspectParams proto.Message

	// AspectValidator describes a type that is able to validate Aspect configuration.
	AspectValidator interface {
		// DefaultConfig returns a default configuration struct for this
		// adapter. This will be used by the configuration system to establish
		// the shape of the block of configuration state passed to the NewAspect method.
		DefaultConfig() (c AspectParams)

		// ValidateConfig determines whether the given configuration meets all correctness requirements.
		ValidateConfig(c AspectParams, validator expr.Validator, finder descriptor.Finder) *adapter.ConfigErrors
	}

	// BuilderValidatorFinder is used to find specific underlying validators.
	// Manager registry and adapter registry should implement this interface
	// so ConfigValidators can be uniformly accessed.
	BuilderValidatorFinder func(name string) (adapter.ConfigValidator, bool)

	// AspectValidatorFinder is used to find specific underlying validators.
	// Manager registry and adapter registry should implement this interface
	// so ConfigValidators can be uniformly accessed.
	AspectValidatorFinder func(kind Kind) (AspectValidator, bool)

	// AdapterToAspectMapper returns the set of aspect kinds implemented by
	// the given builder.
	AdapterToAspectMapper func(builder string) KindSet
)

// newValidator returns a validator given component validators.
func newValidator(managerFinder AspectValidatorFinder, adapterFinder BuilderValidatorFinder,
	findAspects AdapterToAspectMapper, strict bool, exprValidator expr.Validator) *validator {
	return &validator{
		managerFinder: managerFinder,
		adapterFinder: adapterFinder,
		findAspects:   findAspects,
		strict:        strict,
		exprValidator: exprValidator,
		validated: &Validated{
			adapterByName: make(map[adapterKey]*pb.Adapter),
			rule:          make(map[rulesKey]*pb.ServiceConfig),
			adapter:       make(map[string]*pb.GlobalConfig),
			descriptor:    make(map[string]*pb.GlobalConfig),
			shas:          make(map[string][sha1.Size]byte),
		},
	}
}

type (
	// validator is the Configuration validator.
	validator struct {
		managerFinder    AspectValidatorFinder
		adapterFinder    BuilderValidatorFinder
		findAspects      AdapterToAspectMapper
		descriptorFinder descriptor.Finder
		strict           bool
		exprValidator    expr.Validator
		validated        *Validated
	}

	adapterKey struct {
		kind Kind
		name string
	}

	// rulesKey is used to lookup the combined rules document.
	rulesKey struct {
		// Scope of the rules document.
		Scope string
		// Subject of the rules document.
		Subject string
	}

	// Validated store validated configuration.
	// It has been validated as internally consistent and correct.
	Validated struct {
		adapterByName map[adapterKey]*pb.Adapter
		// descriptors and adapters are only allowed in global scope
		adapter    map[string]*pb.GlobalConfig
		descriptor map[string]*pb.GlobalConfig
		rule       map[rulesKey]*pb.ServiceConfig
		shas       map[string][sha1.Size]byte
		numAspects int
	}
)

func copyDescriptors(m map[string]*pb.GlobalConfig) map[string]*pb.GlobalConfig {
	d := map[string]*pb.GlobalConfig{}
	for k, a := range m {
		d[k] = a
	}
	return d
}

// Clone makes a clone of validated config
func (v *Validated) Clone() *Validated {
	aa := map[adapterKey]*pb.Adapter{}
	for k, a := range v.adapterByName {
		aa[k] = a
	}

	rule := map[rulesKey]*pb.ServiceConfig{}
	for k, a := range v.rule {
		rule[k] = a
	}

	shas := map[string][sha1.Size]byte{}
	for k, a := range v.shas {
		shas[k] = a
	}

	return &Validated{
		adapterByName: aa,
		rule:          rule,
		adapter:       copyDescriptors(v.adapter),
		descriptor:    copyDescriptors(v.descriptor),
		numAspects:    v.numAspects,
		shas:          shas,
	}
}

const (
	global      = "global"
	scopes      = "scopes"
	subjects    = "subjects"
	rules       = "rules"
	adapters    = "adapters"
	descriptors = "descriptors"

	keyAdapters            = "/scopes/global/adapters"
	keyDescriptors         = "/scopes/global/descriptors"
	keyGlobalServiceConfig = "/scopes/global/subjects/global/rules"
)

// String string representation of a Key
func (p rulesKey) String() string {
	return fmt.Sprintf("%s/%s", p.Scope, p.Subject)
}

// /scopes/global/subjects/global/rules --> global / global
func parseRulesKey(key string) (k *rulesKey) {
	comps := strings.Split(key, "/")
	if len(comps) < 6 {
		return nil
	}
	if comps[1] != scopes || comps[3] != subjects {
		return nil
	}
	k = &rulesKey{comps[2], comps[4]}
	return k
}

func (a adapterKey) String() string {
	return fmt.Sprintf("%s//%s", a.kind, a.name)
}

// FIXME post alpha
// create new messages of type
// message MetricList {
//   repeated metrics = 1;
// }
// One for each type of descriptor
// Those messages can be parsed directly using proto.jsonp.
// At present globalConfig.Adapters contains `struct` that prevents us from using proto.jsonp

// compatfilterConfig
// given a yaml file, filter specific keys from it
// globalConfig contains descriptors and adapters which will be split shortly.
func compatfilterConfig(cfg string, shouldSelect func(string) bool) (data []byte, err error) {
	var m = map[string]interface{}{}

	if err = yaml.Unmarshal([]byte(cfg), &m); err != nil {
		return
	}
	for k := range m {
		if !shouldSelect(k) {
			delete(m, k)
		}
	}
	return json.Marshal(m)
}

// validateDescriptors
//
// Enums as struct fields can be symbolic names.
// However enums inside maps *cannot* be symbolic names.
// TODO add validation beyond proto parse
func (p *validator) validateDescriptors(key string, cfg string) (ce *adapter.ConfigErrors) {
	var err error
	var data []byte

	if data, err = compatfilterConfig(cfg, func(s string) bool {
		return s != "adapters"
	}); err != nil {
		return ce.Appendf("DescriptorConfig", "failed to unmarshal config into proto with err: %v", err)
	}
	m := &pb.GlobalConfig{}
	um := jsonpb.Unmarshaler{AllowUnknownFields: true}

	if err = um.Unmarshal(bytes.NewReader(data), m); err != nil {
		return ce.Appendf("DescriptorConfig", "failed to unmarshal <%s> config into proto with err: %v", string(data), err)
	}

	p.validated.descriptor[key] = m
	return
}

// validateAdapters consumes a yml config string with adapter config.
// It is validated in the presence of validators.
func (p *validator) validateAdapters(key string, cfg string) (ce *adapter.ConfigErrors) {
	var ferr error
	var data []byte

	if data, ferr = compatfilterConfig(cfg, func(s string) bool {
		return s == "adapters"
	}); ferr != nil {
		return ce.Appendf("DescriptorConfig", "failed to unmarshal config into proto with err: %v", ferr)
	}

	var m = &pb.GlobalConfig{}
	if err := yaml.Unmarshal(data, m); err != nil {
		return ce.Appendf("GlobalConfig", "failed to unmarshal config into proto with err: %v", err)
	}

	var acfg adapter.Config
	var err *adapter.ConfigErrors
	// FIXME update this when we start supporting adapters defined in multiple scopes
	p.validated.adapterByName = make(map[adapterKey]*pb.Adapter)
	for _, aa := range m.GetAdapters() {
		if acfg, err = convertAdapterParams(p.adapterFinder, aa.Impl, aa.Params, p.strict); err != nil {
			ce = ce.Appendf("Adapter: "+aa.Impl, "failed to convert aspect params to proto with err: %v", err)
			continue
		}
		aa.Params = acfg
		// check which kinds aa.Impl provides
		// Then register it for all of them.
		kinds := p.findAspects(aa.Impl)
		for kind := Kind(0); kind < NumKinds; kind++ {
			if kinds.IsSet(kind) {
				p.validated.adapterByName[adapterKey{kind, aa.Name}] = aa
			}
		}
	}
	p.validated.adapter[key] = m
	return
}

// ValidateSelector ensures that the selector is valid per expression language.
func (p *validator) validateSelector(selector string) (err error) {
	// empty selector always selects
	if len(selector) == 0 {
		return nil
	}
	return p.exprValidator.Validate(selector)
}

// validateAspectRules validates the recursive configuration data structure.
// It is primarily used by validate ServiceConfig.
func (p *validator) validateAspectRules(rules []*pb.AspectRule, path string, validatePresence bool) (numAspects int, ce *adapter.ConfigErrors) {
	var acfg adapter.Config
	for _, rule := range rules {
		if err := p.validateSelector(rule.GetSelector()); err != nil {
			ce = ce.Append(path+":Selector "+rule.GetSelector(), err)
		}
		var err *adapter.ConfigErrors
		path = path + "/" + rule.GetSelector()
		for idx, aa := range rule.GetAspects() {
			if acfg, err = convertAspectParams(p.managerFinder, aa.Kind, aa.GetParams(), p.strict, p.descriptorFinder); err != nil {
				ce = ce.Appendf(fmt.Sprintf("%s:%s[%d]", path, aa.Kind, idx), "failed to parse params with err: %v", err)
				continue
			}
			aa.Params = acfg
			numAspects++
			if validatePresence {
				if aa.Adapter == "" {
					aa.Adapter = "default"
				}
				// ensure that aa.Kind has a registered adapter
				k, ok := ParseKind(aa.Kind)
				if !ok {
					ce = ce.Appendf("Kind", "%s is not a valid kind", aa.Kind)
				} else {
					ak := adapterKey{k, aa.Adapter}
					if p.validated.adapterByName[ak] == nil {
						ce = ce.Appendf("NamedAdapter", "%s not available", ak)
					}
				}
			}
		}
		rs := rule.GetRules()
		if len(rs) == 0 {
			continue
		}
		if na, verr := p.validateAspectRules(rs, path, validatePresence); verr != nil {
			ce = ce.Extend(verr)
		} else {
			numAspects += na
		}
	}
	return numAspects, ce
}

//  classify keys into rules, adapter and descriptors

func classifyKeys(keys []string) map[string][]string {
	keymap := map[string][]string{}
	for _, key := range keys {
		kk := strings.Split(key, "/")
		var k string
		switch kk[len(kk)-1] {
		case rules:
			k = rules
		case adapters:
			k = adapters
		case descriptors:
			k = descriptors
		default:
			if glog.V(4) {
				glog.Infoln("unknown key", keys)
			}
			continue
		}
		keymap[k] = append(keymap[k], key)
	}

	return keymap
}

func descriptorKey(scope string) string {
	return fmt.Sprintf("/scopes/%s/%s", scope, descriptors)
}

// validate validates a single serviceConfig and globalConfig together.
// It returns a fully validated Config if no errors are found.
func (p *validator) validate(cfg map[string]string) (rt *Validated, ce *adapter.ConfigErrors) {

	cfgkey := make([]string, len(cfg))
	for k := range cfg {
		cfgkey = append(cfgkey, k)
	}
	keymap := classifyKeys(cfgkey)

	for _, kk := range keymap[descriptors] {
		if re := p.validateDescriptors(kk, cfg[kk]); re != nil {
			return rt, ce.Appendf("GlobalConfig", "failed validation").Extend(re)
		}
	}

	for _, kk := range keymap[adapters] {
		if re := p.validateAdapters(kk, cfg[kk]); re != nil {
			return rt, ce.Appendf("GlobalConfig", "failed validation").Extend(re)
		}
	}

	// The order is important here, because serviceConfig refers to adapters and descriptors
	p.descriptorFinder = descriptor.NewFinder(p.validated.descriptor[descriptorKey(global)])
	for _, kk := range keymap[rules] {
		ck := parseRulesKey(kk)
		if ck == nil {
			continue
		}
		if re := p.validateServiceConfig(*ck, cfg[kk], true); re != nil {
			return rt, ce.Appendf("ServiceConfig", "failed validation").Extend(re)
		}
	}
	return p.validated, nil
}

// ValidateServiceConfig validates service config.
// if validatePresence is true it will ensure that the named adapter and Kinds
// have an available and configured adapter.
func (p *validator) validateServiceConfig(pk rulesKey, cfg string, validatePresence bool) (ce *adapter.ConfigErrors) {
	var err error
	m := &pb.ServiceConfig{}
	var numAspects int
	if err = yaml.Unmarshal([]byte(cfg), m); err != nil {
		return ce.Appendf("ServiceConfig", "failed to unmarshal config into proto with err: %v", err)
	}

	if numAspects, ce = p.validateAspectRules(m.GetRules(), "", validatePresence); ce != nil {
		return ce
	}
	p.validated.rule[pk] = m
	p.validated.numAspects += numAspects

	return nil
}

// unknownValidator returns error for the given name.
func unknownValidator(name string) error {
	return fmt.Errorf("unknown type [%s]", name)
}

// unknownKind returns error for the given name.
func unknownKind(name string) error {
	return fmt.Errorf("unknown aspect kind [%s]", name)
}

// convertAdapterParams converts returns a typed proto message based on available validator.
func convertAdapterParams(f BuilderValidatorFinder, name string, params interface{}, strict bool) (ac adapter.Config, ce *adapter.ConfigErrors) {
	var avl adapter.ConfigValidator
	var found bool

	if avl, found = f(name); !found {
		return nil, ce.Append(name, unknownValidator(name))
	}

	ac = avl.DefaultConfig()
	if err := decode(params, ac, strict); err != nil {
		return nil, ce.Appendf(name, "failed to decode adapter params with err: %v", err)
	}
	if err := avl.ValidateConfig(ac); err != nil {
		return nil, ce.Appendf(name, "adapter validation failed with err: %v", err)
	}
	return ac, nil
}

// convertAspectParams converts returns a typed proto message based on available validator.
func convertAspectParams(f AspectValidatorFinder, name string, params interface{}, strict bool, df descriptor.Finder) (AspectParams, *adapter.ConfigErrors) {
	var ce *adapter.ConfigErrors
	var avl AspectValidator
	var found bool
	var k Kind

	if k, found = ParseKind(name); !found {
		return nil, ce.Append(name, unknownKind(name))
	}

	if avl, found = f(k); !found {
		return nil, ce.Append(name, unknownValidator(name))
	}

	ap := avl.DefaultConfig()
	if err := decode(params, ap, strict); err != nil {
		return nil, ce.Appendf(name, "failed to decode aspect params with err: %v", err)
	}
	if err := avl.ValidateConfig(ap, expr.NewCEXLEvaluator(), df); err != nil {
		return nil, ce.Appendf(name, "aspect validation failed with err: %v", err)
	}
	return ap, nil
}

// decode interprets src interface{} as the specified proto message.
// if strict is true returns error on unknown fields.
func decode(src interface{}, dst proto.Message, strict bool) error {
	ba, err := json.Marshal(src)
	if err != nil {
		return fmt.Errorf("failed to marshal config into json with err: %v", err)
	}
	um := jsonpb.Unmarshaler{AllowUnknownFields: !strict}
	if err := um.Unmarshal(bytes.NewReader(ba), dst); err != nil {
		return fmt.Errorf("failed to unmarshal config into proto with err: %v", err)
	}
	return nil
}
