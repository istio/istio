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

package aspect

import (
	"fmt"

	"google.golang.org/genproto/googleapis/rpc/code"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"
)

type (
	listCheckerManager struct{}

	listCheckerWrapper struct {
		cfg          *CombinedConfig
		adapter      adapter.ListCheckerAdapter
		aspect       adapter.ListCheckerAspect
		aspectConfig *config.ListCheckerParams
	}
)

// NewListCheckerManager returns "this" aspect Manager
func NewListCheckerManager() Manager {
	return &listCheckerManager{}
}

// NewAspect creates a listChecker aspect.
func (m *listCheckerManager) NewAspect(cfg *CombinedConfig, ga adapter.Adapter, env adapter.Env) (Wrapper, error) {
	aa, ok := ga.(adapter.ListCheckerAdapter)
	if !ok {
		return nil, fmt.Errorf("adapter of incorrect type; expected adapter.ListCheckerAdapter got %#v %T", ga, ga)
	}

	// TODO: convert from proto Struct to Go struct here!
	var aspectConfig *config.ListCheckerParams
	adapterCfg := aa.DefaultConfig()
	// TODO: parse cfg.Adapter.Params (*ptypes.struct) into adapterCfg
	var asp adapter.ListCheckerAspect
	var err error

	if asp, err = aa.NewListChecker(env, adapterCfg); err != nil {
		return nil, err
	}

	return &listCheckerWrapper{
		cfg:          cfg,
		adapter:      aa,
		aspect:       asp,
		aspectConfig: aspectConfig,
	}, nil
}

func (*listCheckerManager) Kind() string {
	return "istio/listChecker"
}

func (*listCheckerManager) DefaultConfig() adapter.AspectConfig {
	return &config.ListCheckerParams{
		CheckAttribute: "src.ip",
	}
}

func (*listCheckerManager) ValidateConfig(c adapter.AspectConfig) (ce *adapter.ConfigErrors) {
	lc := c.(*config.ListCheckerParams)
	if lc.CheckAttribute == "" {
		ce = ce.Appendf("check_attribute", "Missing")
	}
	return
}

func (a *listCheckerWrapper) AdapterName() string {
	return a.adapter.Name()
}

func (a *listCheckerWrapper) Execute(attrs attribute.Bag, mapper expr.Evaluator) (*Output, error) {
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

	return &Output{Code: rCode}, nil
}
