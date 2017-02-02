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
	aconfig "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/expr"
)

type (
	listCheckerManager struct{}

	listCheckerWrapper struct {
		inputs map[string]string
		aspect adapter.ListCheckerAspect
		params *aconfig.ListCheckerParams
	}
)

// NewListCheckerManager returns "this" aspect Manager
func NewListCheckerManager() Manager {
	return listCheckerManager{}
}

// NewAspect creates a listChecker aspect.
func (listCheckerManager) NewAspect(cfg *config.Combined, ga adapter.Builder, env adapter.Env) (Wrapper, error) {
	aa := ga.(adapter.ListCheckerBuilder)
	var asp adapter.ListCheckerAspect
	var err error

	if asp, err = aa.NewListChecker(env, cfg.Builder.Params.(adapter.AspectConfig)); err != nil {
		return nil, err
	}
	return &listCheckerWrapper{
		inputs: cfg.Aspect.Inputs,
		aspect: asp,
		params: cfg.Aspect.Params.(*aconfig.ListCheckerParams),
	}, nil
}

func (listCheckerManager) Kind() string {
	return ListKind
}

func (listCheckerManager) DefaultConfig() adapter.AspectConfig {
	return &aconfig.ListCheckerParams{
		CheckAttribute: "src.ip",
	}
}

func (listCheckerManager) ValidateConfig(c adapter.AspectConfig) (ce *adapter.ConfigErrors) {
	lc := c.(*aconfig.ListCheckerParams)
	if lc.CheckAttribute == "" {
		ce = ce.Appendf("CheckAttribute", "Missing")
	}
	return
}

func (a *listCheckerWrapper) Execute(attrs attribute.Bag, mapper expr.Evaluator) (*Output, error) {
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

func (a *listCheckerWrapper) Close() error { return a.aspect.Close() }
