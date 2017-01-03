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
	listcheckerpb "istio.io/api/istio/config/v1/aspect/listChecker"
	"istio.io/mixer/pkg/aspect"
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
		aspect       aspect.Aspect
		aspectConfig *listcheckerpb.Config
	}
)

// NewManager returns "this" aspect Manager
func NewManager() aspect.Manager {
	return &manager{}
}

// NewAspect creates a listChecker aspect. Implements aspect.Manager#NewAspect()
func (m *manager) NewAspect(cfg *aspect.CombinedConfig, ga aspect.Adapter) (aspect.AspectWrapper, error) {
	aa, ok := ga.(Adapter)
	if !ok {
		return nil, fmt.Errorf("Adapter of incorrect type. Expected listChecker.Adapter got %#v %T", ga, ga)
	}

	// TODO: convert from proto Struct to Go struct here!
	var aspectConfig *listcheckerpb.Config
	adapterCfg := aa.DefaultConfig()
	// TODO: parse cfg.Adapter.Params (*ptypes.struct) into adapterCfg
	var asp aspect.Aspect
	var err error


	if asp, err = aa.NewAspect(adapterCfg); err != nil {
		return nil, err
	}

	return &aspectWrapper{
		cfg:          cfg,
		aspect:       asp,
		aspectConfig: aspectConfig,
	}, nil
}

func (a *aspectWrapper) Execute(attrs attribute.Bag, mapper expr.Evaluator) (*aspect.Output, error) {
	var found bool
	var err error
	var asp Aspect

	var symbol string
	var symbolExpr string
	acfg := a.aspectConfig

	// CheckAttribute should be processed and sent to input
	if symbolExpr, found = a.cfg.Aspect.Inputs[acfg.CheckAttribute]; !found {
		return nil, fmt.Errorf("Mapping for %s not found", acfg.CheckAttribute)
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

func (*manager) Kind() string {
	return kind
}
