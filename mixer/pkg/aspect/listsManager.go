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

	apipb "istio.io/api/mixer/v1/config/descriptor"
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
		aspect adapter.ListsAspect
		params *aconfig.ListsParams
	}
)

// newListsManager returns a manager for the lists aspect.
func newListsManager() CheckManager {
	return listsManager{}
}

// NewCheckExecutor creates a listChecker aspect.
func (listsManager) NewCheckExecutor(cfg *cpb.Combined, createAspect CreateAspectFunc, env adapter.Env,
	df descriptor.Finder, _ string) (CheckExecutor, error) {
	out, err := createAspect(env, cfg.Builder.Params.(config.AspectParams))
	if err != nil {
		return nil, err
	}
	asp, ok := out.(adapter.ListsAspect)
	if !ok {
		return nil, fmt.Errorf("wrong aspect type returned after creation; expected ListsAspect: %#v", out)
	}
	return &listsExecutor{
		aspect: asp,
		params: cfg.Aspect.Params.(*aconfig.ListsParams),
	}, nil
}

func (listsManager) Kind() config.Kind {
	return config.ListsKind
}

func (listsManager) DefaultConfig() config.AspectParams {
	return &aconfig.ListsParams{
		CheckExpression: "source.ip",
	}
}

func (listsManager) ValidateConfig(c config.AspectParams, tc expr.TypeChecker, df descriptor.Finder) (ce *adapter.ConfigErrors) {
	cfg := c.(*aconfig.ListsParams)
	if cfg.CheckExpression == "" {
		ce = ce.Appendf("checkExpression", "no expression provided")
	} else if err := tc.AssertType(cfg.CheckExpression, df, apipb.STRING); err != nil {
		ce = ce.Appendf("checkExpression", "error type checking expression: %v", err)
	}
	return
}

func (a *listsExecutor) Execute(attrs attribute.Bag, mapper expr.Evaluator) rpc.Status {
	var found bool
	var err error

	var symbol string
	if symbol, err = mapper.EvalString(a.params.CheckExpression, attrs); err != nil {
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
