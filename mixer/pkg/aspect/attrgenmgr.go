// Copyright 2017 Istio Authors.
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
	"net"

	"github.com/golang/glog"
	rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/istio/mixer/pkg/adapter"
	apb "istio.io/istio/mixer/pkg/aspect/config"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/config"
	"istio.io/istio/mixer/pkg/config/descriptor"
	cpb "istio.io/istio/mixer/pkg/config/proto"
	"istio.io/istio/mixer/pkg/expr"
	"istio.io/istio/mixer/pkg/status"
)

type (
	attrGenMgr  struct{}
	attrGenExec struct {
		aspect   adapter.AttributesGenerator
		params   *apb.AttributesGeneratorParams
		bindings map[string]string // value name => attribute name
	}
)

func newAttrGenMgr() PreprocessManager {
	return &attrGenMgr{}
}

func (attrGenMgr) Kind() config.Kind {
	return config.AttributesKind
}

func (attrGenMgr) DefaultConfig() (c config.AspectParams) {
	// NOTE: The default config leads to the generation of no new attributes.
	return &apb.AttributesGeneratorParams{}
}

func (attrGenMgr) ValidateConfig(c config.AspectParams, tc expr.TypeChecker, df descriptor.Finder) (cerrs *adapter.ConfigErrors) {
	params := c.(*apb.AttributesGeneratorParams)
	for n, expr := range params.InputExpressions {
		if _, err := tc.EvalType(expr, df); err != nil {
			cerrs = cerrs.Appendf("inputExpressions", "failed to parse expression '%s': %v", n, err)
		}
	}

	for attrName := range params.AttributeBindings {
		if a := df.GetAttribute(attrName); a == nil {
			cerrs = cerrs.Appendf(
				"attributeBindings",
				"Attribute '%s' is not configured for use within Mixer. It cannot be used as a target for generated values.",
				attrName)
		}
	}
	return
}

func (attrGenMgr) NewPreprocessExecutor(cfg *cpb.Combined, createAspect CreateAspectFunc, env adapter.Env, df descriptor.Finder) (PreprocessExecutor, error) {
	out, err := createAspect(env, cfg.Builder.Params.(config.AspectParams))
	if err != nil {
		return nil, err
	}
	asp, ok := out.(adapter.AttributesGenerator)
	if !ok {
		return nil, fmt.Errorf("wrong aspect type returned after creation; expected AttributesGenerator: %#v", out)
	}
	params := cfg.Aspect.Params.(*apb.AttributesGeneratorParams)
	bindings := make(map[string]string, len(params.AttributeBindings))
	for attrName, valName := range params.AttributeBindings {
		bindings[valName] = attrName
	}
	return &attrGenExec{aspect: asp, params: params, bindings: bindings}, nil
}

func (e *attrGenExec) Execute(attrs attribute.Bag, mapper expr.Evaluator) (*PreprocessResult, rpc.Status) {
	attrGen := e.aspect
	in, err := evalAll(e.params.InputExpressions, attrs, mapper)
	if err != nil {
		errMsg := "Could not evaluate input expressions for attribute generation."
		glog.Error(errMsg, err)
		return nil, status.WithInternal(errMsg)
	}
	out, err := attrGen.Generate(in)
	if err != nil {
		errMsg := "Attribute value generation failed."
		glog.Error(errMsg, err)
		return nil, status.WithInternal(errMsg)
	}
	bag := attribute.GetMutableBag(nil)
	for key, val := range out {
		if attrName, found := e.bindings[key]; found {
			// TODO: type validation?
			switch v := val.(type) {
			case net.IP:
				// conversion to []byte necessary based on current IP_ADDRESS handling within Mixer
				// TODO: remove
				glog.V(4).Info("converting net.IP to []byte")
				if v4 := v.To4(); v4 != nil {
					bag.Set(attrName, []byte(v4))
					continue
				}
				bag.Set(attrName, []byte(v.To16()))
			default:
				bag.Set(attrName, val)
			}
			continue
		}
		if glog.V(4) {
			glog.Infof("Generated value '%s' was not mapped to an attribute.", key)
		}
	}
	// TODO: check that all attributes in map have been assigned a value?
	return &PreprocessResult{Attrs: bag}, status.OK
}

func (e *attrGenExec) Close() error { return e.aspect.Close() }
