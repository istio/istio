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

	"google.golang.org/genproto/googleapis/rpc/code"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"
)

const (
	kind = "istio/denyChecker"
)

type (
	manager struct{}

	aspectWrapper struct {
		aspect Aspect
	}
)

// NewManager returns "this" aspect Manager
func NewManager() aspect.Manager {
	return &manager{}
}

// NewAspect creates a denyChecker aspect. Implements aspect.Manager#NewAspect()
func (m *manager) NewAspect(cfg *aspect.CombinedConfig, ga aspect.Adapter) (aspect.AspectWrapper, error) {
	aa, ok := ga.(Adapter)
	if !ok {
		return nil, fmt.Errorf("Adapter of incorrect type. Expected denyChecker.Adapter got %#v %T", ga, ga)
	}

	// TODO: convert from proto Struct to Go struct here!
	adapterCfg := aa.DefaultConfig()
	// TODO: parse cfg.Adapter.Params (*ptypes.struct) into adapterCfg
	var asp Aspect
	var err error

	if asp, err = aa.NewAspect(adapterCfg); err != nil {
		return nil, err
	}

	return &aspectWrapper{
		aspect: asp,
	}, nil
}

func (a *aspectWrapper) Execute(attrs attribute.Bag, mapper expr.Evaluator) (*aspect.Output, error) {
	status := a.aspect.Deny()
	return &aspect.Output{Code: code.Code(status.Code)}, nil
}

func (*manager) Kind() string {
	return kind
}
