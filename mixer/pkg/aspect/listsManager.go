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

	"google.golang.org/genproto/googleapis/rpc/code"

	"istio.io/mixer/pkg/adapter"
	aconfig "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/expr"
)

type (
	listsManager struct{}

	listsWrapper struct {
		inputs map[string]string
		aspect adapter.ListsAspect
		params *aconfig.ListsParams
	}
)

// NewListsManager returns a manager for the lists aspect.
func NewListsManager() Manager {
	return listsManager{}
}

// NewAspect creates a listChecker aspect.
func (listsManager) NewAspect(cfg *config.Combined, ga adapter.Builder, env adapter.Env) (Wrapper, error) {
	aa := ga.(adapter.ListsBuilder)
	var asp adapter.ListsAspect
	var err error

	if asp, err = aa.NewListsAspect(env, cfg.Builder.Params.(adapter.AspectConfig)); err != nil {
		return nil, err
	}
	return &listsWrapper{
		inputs: cfg.Aspect.Inputs,
		aspect: asp,
		params: cfg.Aspect.Params.(*aconfig.ListsParams),
	}, nil
}

func (listsManager) Kind() Kind {
	return ListsKind
}

func (listsManager) DefaultConfig() adapter.AspectConfig {
	return &aconfig.ListsParams{
		CheckAttribute: "src.ip",
	}
}

func (listsManager) ValidateConfig(c adapter.AspectConfig) (ce *adapter.ConfigErrors) {
	lc := c.(*aconfig.ListsParams)
	if lc.CheckAttribute == "" {
		ce = ce.Appendf("CheckAttribute", "Missing")
	}
	return
}

func (a *listsWrapper) Execute(attrs attribute.Bag, mapper expr.Evaluator, ma APIMethodArgs) (*Output, error) {
	var found bool
	var err error

	var symbol string
	var symbolExpr string

	// CheckAttribute should be processed and sent to input
	if symbolExpr, found = a.inputs[a.params.CheckAttribute]; !found {
		return nil, fmt.Errorf("mapping for %s not found", a.params.CheckAttribute)
	}

	if symbol, err = mapper.EvalString(symbolExpr, attrs); err != nil {
		return nil, err
	}

	if found, err = a.aspect.CheckList(symbol); err != nil {
		return nil, err
	}
	rCode := code.Code_PERMISSION_DENIED

	if found != a.params.Blacklist {
		rCode = code.Code_OK
	}
	return &Output{Code: rCode}, nil
}

func (a *listsWrapper) Close() error { return a.aspect.Close() }
