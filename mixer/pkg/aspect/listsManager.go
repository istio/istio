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
	"fmt"

	rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/mixer/pkg/adapter"
	aconfig "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/config/descriptor"
	cpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/status"
)

type (
	listsManager struct{}

	listsExecutor struct {
		inputs map[string]string
		aspect adapter.ListsAspect
		params *aconfig.ListsParams
	}
)

// newListsManager returns a manager for the lists aspect.
func newListsManager() CheckManager {
	return listsManager{}
}

// NewCheckExecutor creates a listChecker aspect.
func (listsManager) NewCheckExecutor(cfg *cpb.Combined, ga adapter.Builder, env adapter.Env, df descriptor.Finder) (CheckExecutor, error) {
	aa := ga.(adapter.ListsBuilder)
	var asp adapter.ListsAspect
	var err error

	if asp, err = aa.NewListsAspect(env, cfg.Builder.Params.(config.AspectParams)); err != nil {
		return nil, err
	}
	return &listsExecutor{
		inputs: cfg.Aspect.Inputs,
		aspect: asp,
		params: cfg.Aspect.Params.(*aconfig.ListsParams),
	}, nil
}

func (listsManager) Kind() Kind {
	return ListsKind
}

func (listsManager) DefaultConfig() config.AspectParams {
	return &aconfig.ListsParams{
		CheckAttribute: "src.ip",
	}
}

func (listsManager) ValidateConfig(c config.AspectParams, _ expr.Validator, _ descriptor.Finder) (ce *adapter.ConfigErrors) {
	lc := c.(*aconfig.ListsParams)
	if lc.CheckAttribute == "" {
		ce = ce.Appendf("CheckAttribute", "Missing")
	}
	return
}

func (a *listsExecutor) Execute(attrs attribute.Bag, mapper expr.Evaluator) rpc.Status {
	var found bool
	var err error

	var symbol string
	var symbolExpr string

	// CheckAttribute should be processed and sent to input
	if symbolExpr, found = a.inputs[a.params.CheckAttribute]; !found {
		return status.WithError(fmt.Errorf("mapping for %s not found", a.params.CheckAttribute))
	}

	if symbol, err = mapper.EvalString(symbolExpr, attrs); err != nil {
		return status.WithError(err)
	}

	if found, err = a.aspect.CheckList(symbol); err != nil {
		return status.WithError(err)
	}

	if found != a.params.Blacklist {
		return status.OK
	}
	return status.WithPermissionDenied(fmt.Sprintf("%s rejected", symbol))
}

func (a *listsExecutor) Close() error { return a.aspect.Close() }
