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
	"google.golang.org/genproto/googleapis/rpc/code"

	"istio.io/mixer/pkg/adapter"
	aconfig "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/expr"
)

type (
	denyCheckerManager struct{}

	denyCheckerWrapper struct {
		aspect adapter.DenyCheckerAspect
	}
)

// NewDenyCheckerManager returns a DenyCheckerManager.
func NewDenyCheckerManager() Manager {
	return denyCheckerManager{}
}

// NewAspect creates a denyChecker aspect.
func (denyCheckerManager) NewAspect(cfg *config.Combined, ga adapter.Builder, env adapter.Env) (Wrapper, error) {
	aa := ga.(adapter.DenyCheckerBuilder)
	var asp adapter.DenyCheckerAspect
	var err error

	if asp, err = aa.NewDenyChecker(env, cfg.Builder.Params.(adapter.AspectConfig)); err != nil {
		return nil, err
	}

	return &denyCheckerWrapper{
		aspect: asp,
	}, nil
}

func (denyCheckerManager) Kind() string                                                     { return "istio/denyChecker" }
func (denyCheckerManager) DefaultConfig() adapter.AspectConfig                              { return &aconfig.DenyCheckerParams{} }
func (denyCheckerManager) ValidateConfig(c adapter.AspectConfig) (ce *adapter.ConfigErrors) { return }

func (a *denyCheckerWrapper) Execute(attrs attribute.Bag, mapper expr.Evaluator) (*Output, error) {
	status := a.aspect.Deny()
	return &Output{Code: code.Code(status.Code)}, nil
}

func (a *denyCheckerWrapper) Close() error { return a.aspect.Close() }
