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
// Validator
// 1. Accepts new configuration from user
// 2. Validates configuration
// 3. Produces a "ValidatedConfig"
// Runtime
// 1. It is validated and actionable configuration
// 2. It resolves the configuration to a list of Combined {aspect, adapter} configs
//    given an attribute.Bag.
// 3. Combined config has complete information needed to dispatch aspect
package config

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"
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
		ValidateConfig(c AspectParams, finder descriptor.Finder) *adapter.ConfigErrors
	}

	// AdapterValidatorFinder is used to find specific underlying validators.
	// Manager registry and adapter registry should implement this interface
	// so ConfigValidators can be uniformly accessed.
	AdapterValidatorFinder func(name string) (adapter.ConfigValidator, bool)

	// AspectValidatorFinder is used to find specific underlying validators.
	// Manager registry and adapter registry should implement this interface
	// so ConfigValidators can be uniformly accessed.
	AspectValidatorFinder func(name string) (AspectValidator, bool)

	// AdapterToAspectMapper given an pb.Adapter.Impl
	// This is specifically *not* querying by pb.Adapter.Name
	// returns a list of aspects the adapter provides.
	// For many adapter this will be exactly 1 aspect.
	AdapterToAspectMapper func(impl string) []string
)

// NewValidator returns a validator given component validators.
func NewValidator(managerFinder AspectValidatorFinder, adapterFinder AdapterValidatorFinder,
	findAspects AdapterToAspectMapper, strict bool, exprValidator expr.Validator) *Validator {
	return &Validator{
		managerFinder: managerFinder,
		adapterFinder: adapterFinder,
		findAspects:   findAspects,
		strict:        strict,
		exprValidator: exprValidator,
		validated:     &Validated{},
	}
}

type (
	// Validator is the Configuration validator.
	Validator struct {
		managerFinder    AspectValidatorFinder
		adapterFinder    AdapterValidatorFinder
		findAspects      AdapterToAspectMapper
		descriptorFinder descriptor.Finder
		strict           bool
		exprValidator    expr.Validator
		validated        *Validated
	}

	adapterKey struct {
		kind string
		name string
	}
	// Validated store validated configuration.
	// It has been validated as internally consistent and correct.
	Validated struct {
		adapterByName map[adapterKey]*pb.Adapter
		globalConfig  *pb.GlobalConfig
		serviceConfig *pb.ServiceConfig
		numAspects    int
	}
)

func (a adapterKey) String() string {
	return fmt.Sprintf("%s//%s", a.kind, a.name)
}

// validateGlobalConfig consumes a yml config string with adapter config.
// It is validated in presence of validators.
func (p *Validator) validateGlobalConfig(cfg string) (ce *adapter.ConfigErrors) {
	var err error
	m := &pb.GlobalConfig{}

	if err = yaml.Unmarshal([]byte(cfg), m); err != nil {
		ce = ce.Append("AdapterConfig", err)
		return
	}
	p.validated.adapterByName = make(map[adapterKey]*pb.Adapter)
	var acfg adapter.Config
	for _, aa := range m.GetAdapters() {
		if acfg, err = ConvertAdapterParams(p.adapterFinder, aa.Impl, aa.Params, p.strict); err != nil {
			ce = ce.Append("Adapter: "+aa.Impl, err)
			continue
		}
		aa.Params = acfg
		// check which kinds aa.Impl provides
		// Then register it for all of them.
		for _, kind := range p.findAspects(aa.Impl) {
			p.validated.adapterByName[adapterKey{kind, aa.Name}] = aa
		}
	}
	p.validated.globalConfig = m
	return
}

// ValidateSelector ensures that the selector is valid per expression language.
func (p *Validator) validateSelector(selector string) (err error) {
	// empty selector always selects
	if len(selector) == 0 {
		return nil
	}
	return p.exprValidator.Validate(selector)
}

// validateAspectRules validates the recursive configuration data structure.
// It is primarily used by validate ServiceConfig.
func (p *Validator) validateAspectRules(rules []*pb.AspectRule, path string, validatePresence bool) (ce *adapter.ConfigErrors) {
	var acfg adapter.Config
	var err error
	for _, rule := range rules {
		if err = p.validateSelector(rule.GetSelector()); err != nil {
			ce = ce.Append(path+":Selector "+rule.GetSelector(), err)
		}
		path = path + "/" + rule.GetSelector()
		for idx, aa := range rule.GetAspects() {
			if acfg, err = ConvertAspectParams(p.managerFinder, aa.Kind, aa.GetParams(), p.strict, p.descriptorFinder); err != nil {
				ce = ce.Append(fmt.Sprintf("%s:%s[%d]", path, aa.Kind, idx), err)
				continue
			}
			aa.Params = acfg
			p.validated.numAspects++
			if validatePresence {
				if aa.Adapter == "" {
					aa.Adapter = "default"
				}
				// ensure that aa.Kind has a registered adapter
				ak := adapterKey{aa.Kind, aa.Adapter}
				if p.validated.adapterByName[ak] == nil {
					ce = ce.Appendf("NamedAdapter", "%s not available", ak)
				}
			}
		}
		rs := rule.GetRules()
		if len(rs) == 0 {
			continue
		}
		if verr := p.validateAspectRules(rs, path, validatePresence); verr != nil {
			ce = ce.Extend(verr)
		}
	}
	return ce
}

// Validate validates a single serviceConfig and globalConfig together.
// It returns a fully validated Config if no errors are found.
func (p *Validator) Validate(serviceCfg string, globalCfg string) (rt *Validated, ce *adapter.ConfigErrors) {
	var cerr *adapter.ConfigErrors
	if re := p.validateGlobalConfig(globalCfg); re != nil {
		cerr = ce.Appendf("GlobalConfig", "failed validation")
		return rt, cerr.Extend(re)
	}
	// The order is important here, because serviceConfig refers to global config
	p.descriptorFinder = descriptor.NewFinder(p.validated.globalConfig)

	if re := p.validateServiceConfig(serviceCfg, true); re != nil {
		cerr = ce.Appendf("ServiceConfig", "failed validation")
		return rt, cerr.Extend(re)
	}

	return p.validated, nil
}

// ValidateServiceConfig validates service config.
// if validatePresence is true it will ensure that the named adapter and Kinds
// have an available and configured adapter.
func (p *Validator) validateServiceConfig(cfg string, validatePresence bool) (ce *adapter.ConfigErrors) {
	var err error
	m := &pb.ServiceConfig{}
	if err = yaml.Unmarshal([]byte(cfg), m); err != nil {
		ce = ce.Append("ServiceConfig", err)
		return
	}
	if ce = p.validateAspectRules(m.GetRules(), "", validatePresence); ce != nil {
		return ce
	}
	p.validated.serviceConfig = m
	return
}

// UnknownValidator returns error for the given name.
func UnknownValidator(name string) error {
	return fmt.Errorf("unknown type [%s]", name)
}

// ConvertAdapterParams converts returns a typed proto message based on available Validator.
func ConvertAdapterParams(find AdapterValidatorFinder, name string, params interface{}, strict bool) (adapter.Config, error) {
	var avl adapter.ConfigValidator
	var found bool

	if avl, found = find(name); !found {
		return nil, UnknownValidator(name)
	}

	acfg := avl.DefaultConfig()
	if err := Decode(params, acfg, strict); err != nil {
		return nil, err
	}
	if verr := avl.ValidateConfig(acfg); verr != nil {
		return nil, verr
	}
	return acfg, nil
}

// ConvertAspectParams converts returns a typed proto message based on available Validator.
func ConvertAspectParams(find AspectValidatorFinder, name string, params interface{}, strict bool, df descriptor.Finder) (AspectParams, error) {
	var avl AspectValidator
	var found bool

	if avl, found = find(name); !found {
		return nil, UnknownValidator(name)
	}

	acfg := avl.DefaultConfig()
	if err := Decode(params, acfg, strict); err != nil {
		return nil, err
	}
	if verr := avl.ValidateConfig(acfg, df); verr != nil {
		return nil, verr
	}
	return acfg, nil
}

// Decode interprets src interface{} as the specified proto message.
// if strict is true returns error on unknown fields.
func Decode(src interface{}, dst adapter.Config, strict bool) (err error) {
	var ba []byte
	ba, err = json.Marshal(src)
	if err != nil {
		return err
	}
	um := jsonpb.Unmarshaler{AllowUnknownFields: !strict}
	return um.Unmarshal(bytes.NewReader(ba), dst)
}
