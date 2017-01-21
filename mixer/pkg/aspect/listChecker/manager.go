// Copyright 2016 Google Inc.
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

package listChecker

import (
	"fmt"

	"google.golang.org/genproto/googleapis/rpc/code"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/aspect/listChecker/config"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"
)

const (
	kind = "istio/listChecker"
)

type (
	manager struct{}

	aspectWrapper struct {
		cfg          *aspect.CombinedConfig
		adapter      adapter.ListCheckerAdapter
		aspect       adapter.ListCheckerAspect
		aspectConfig *config.Params
	}
)

// NewManager returns "this" aspect Manager
func NewManager() aspect.Manager {
	return &manager{}
}

// NewAspect creates a listChecker aspect.
func (m *manager) NewAspect(cfg *aspect.CombinedConfig, ga adapter.Adapter, env adapter.Env) (aspect.Wrapper, error) {
	aa, ok := ga.(adapter.ListCheckerAdapter)
	if !ok {
		return nil, fmt.Errorf("adapter of incorrect type; expected adapter.ListCheckerAdapter got %#v %T", ga, ga)
	}

	// TODO: convert from proto Struct to Go struct here!
	var aspectConfig *config.Params
	adapterCfg := aa.DefaultConfig()
	// TODO: parse cfg.Adapter.Params (*ptypes.struct) into adapterCfg
	var asp adapter.ListCheckerAspect
	var err error

	if asp, err = aa.NewListChecker(env, adapterCfg); err != nil {
		return nil, err
	}

	return &aspectWrapper{
		cfg:          cfg,
		adapter:      aa,
		aspect:       asp,
		aspectConfig: aspectConfig,
	}, nil
}

func (*manager) Kind() string {
	return kind
}

func (*manager) DefaultConfig() adapter.AspectConfig {
	return &config.Params{
		CheckAttribute: "src.ip",
	}
}

func (*manager) ValidateConfig(c adapter.AspectConfig) (ce *adapter.ConfigErrors) {
	lc := c.(*config.Params)
	if lc.CheckAttribute == "" {
		ce = ce.Appendf("check_attribute", "Missing")
	}
	return
}

func (a *aspectWrapper) AdapterName() string {
	return a.adapter.Name()
}

func (a *aspectWrapper) Execute(attrs attribute.Bag, mapper expr.Evaluator) (*aspect.Output, error) {
	var found bool
	var err error
	asp := a.aspect

	var symbol string
	var symbolExpr string
	acfg := a.aspectConfig

	// CheckAttribute should be processed and sent to input
	if symbolExpr, found = a.cfg.Aspect.Inputs[acfg.CheckAttribute]; !found {
		return nil, fmt.Errorf("mapping for %s not found", acfg.CheckAttribute)
	}

	if symbol, err = mapper.EvalString(symbolExpr, attrs); err != nil {
		return nil, err
	}

	if found, err = asp.CheckList(symbol); err != nil {
		return nil, err
	}
	rCode := code.Code_PERMISSION_DENIED

	if found != acfg.Blacklist {
		rCode = code.Code_OK
	}

	return &aspect.Output{Code: rCode}, nil
}
