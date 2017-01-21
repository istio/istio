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
	denyCheckerManager struct{}

	denyCheckerWrapper struct {
		adapter adapter.DenyCheckerAdapter
		aspect  adapter.DenyCheckerAspect
	}
)

// NewDenyCheckerManager returns an instance of the DenyChecker aspect manager.
func NewDenyCheckerManager() Manager {
	return &denyCheckerManager{}
}

// NewAspect creates a denyChecker aspect.
func (m *denyCheckerManager) NewAspect(cfg *CombinedConfig, ga adapter.Adapter, env adapter.Env) (Wrapper, error) {
	aa, ok := ga.(adapter.DenyCheckerAdapter)
	if !ok {
		return nil, fmt.Errorf("adapter of incorrect type; expected adapter.DenyCheckerAdapter got %#v %T", ga, ga)
	}

	// TODO: convert from proto Struct to Go struct here!
	adapterCfg := aa.DefaultConfig()
	// TODO: parse cfg.Adapter.Params (*ptypes.struct) into adapterCfg
	var asp adapter.DenyCheckerAspect
	var err error

	if asp, err = aa.NewDenyChecker(env, adapterCfg); err != nil {
		return nil, err
	}

	return &denyCheckerWrapper{
		adapter: aa,
		aspect:  asp,
	}, nil
}

func (*denyCheckerManager) Kind() string {
	return "istio/denyChecker"
}

func (*denyCheckerManager) DefaultConfig() adapter.AspectConfig {
	return &config.DenyCheckerParams{}
}

func (*denyCheckerManager) ValidateConfig(c adapter.AspectConfig) (ce *adapter.ConfigErrors) {
	return
}

func (a *denyCheckerWrapper) AdapterName() string {
	return a.adapter.Name()
}

func (a *denyCheckerWrapper) Execute(attrs attribute.Bag, mapper expr.Evaluator) (*Output, error) {
	status := a.aspect.Deny()
	return &Output{Code: code.Code(status.Code)}, nil
}
