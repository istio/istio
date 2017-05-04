// Copyright 2016 Istio Authors
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
	rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/mixer/pkg/adapter"
	aconfig "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/config/descriptor"
	cpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
)

type (
	denialsManager struct{}

	denialsExecutor struct {
		aspect adapter.DenialsAspect
	}
)

// newDenialsManager returns a manager for the denials aspect.
func newDenialsManager() CheckManager {
	return denialsManager{}
}

// NewCheckExecutor creates a denyChecker aspect.
func (denialsManager) NewCheckExecutor(cfg *cpb.Combined, ga adapter.Builder, env adapter.Env, df descriptor.Finder) (CheckExecutor, error) {
	aa := ga.(adapter.DenialsBuilder)
	var asp adapter.DenialsAspect
	var err error

	if asp, err = aa.NewDenialsAspect(env, cfg.Builder.Params.(config.AspectParams)); err != nil {
		return nil, err
	}

	return &denialsExecutor{
		aspect: asp,
	}, nil
}

func (denialsManager) Kind() config.Kind                  { return config.DenialsKind }
func (denialsManager) DefaultConfig() config.AspectParams { return &aconfig.DenialsParams{} }
func (denialsManager) ValidateConfig(config.AspectParams, expr.TypeChecker, descriptor.Finder) (ce *adapter.ConfigErrors) {
	return
}

func (a *denialsExecutor) Execute(attrs attribute.Bag, mapper expr.Evaluator) rpc.Status {
	return a.aspect.Deny()
}

func (a *denialsExecutor) Close() error { return a.aspect.Close() }
