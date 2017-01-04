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

package denyChecker

import (
	"fmt"

	denycheckerpb "istio.io/api/istio/config/v1/aspect/denyChecker"
	"google.golang.org/genproto/googleapis/rpc/code"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/aspect/denyChecker"
	"istio.io/mixer/pkg/aspectsupport"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"
	"github.com/golang/protobuf/proto"
)

const (
	kind = "istio/denyChecker"
)

type (
	manager struct{}

	aspectWrapper struct {
		adapter denyChecker.Adapter
		aspect  denyChecker.Aspect
	}
)

// NewManager returns "this" aspect Manager
func NewManager() aspectsupport.Manager {
	return &manager{}
}

// NewAspect creates a denyChecker aspect. Implements aspect.Manager#NewAspect()
func (m *manager) NewAspect(cfg *aspectsupport.CombinedConfig, ga aspect.Adapter, env aspect.Env) (aspectsupport.AspectWrapper, error) {
	aa, ok := ga.(denyChecker.Adapter)
	if !ok {
		return nil, fmt.Errorf("Adapter of incorrect type. Expected denyChecker.Adapter got %#v %T", ga, ga)
	}

	// TODO: convert from proto Struct to Go struct here!
	adapterCfg := aa.DefaultConfig()
	// TODO: parse cfg.Adapter.Params (*ptypes.struct) into adapterCfg
	var asp denyChecker.Aspect
	var err error

	if asp, err = aa.NewAspect(env, adapterCfg); err != nil {
		return nil, err
	}

	return &aspectWrapper{
		adapter: aa,
		aspect:  asp,
	}, nil
}

func (*manager) Kind() string {
	return kind
}

func (*manager) DefaultConfig() proto.Message {
	return &denycheckerpb.Config{}
}

func (*manager) ValidateConfig(implConfig proto.Message) (ce *aspect.ConfigErrors){
	return 
}


func (a *aspectWrapper) AdapterName() string {
	return a.adapter.Name()
}

func (a *aspectWrapper) Execute(attrs attribute.Bag, mapper expr.Evaluator) (*aspectsupport.Output, error) {
	status := a.aspect.Deny()
	return &aspectsupport.Output{Code: code.Code(status.Code)}, nil
}
