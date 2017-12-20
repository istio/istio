// Copyright 2017 Istio Authors
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

package data

import (
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	"istio.io/api/mixer/v1/template"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/il/compiled"
	"istio.io/istio/mixer/pkg/template"
)

// BuildTemplates builds a standard set of testing templates. The supplied override is used to override entries in the
// 't1' templates.
func BuildTemplates(override *template.Info) map[string]*template.Info {
	var t = map[string]*template.Info{
		"t1": {
			Name:    "t1",
			Variety: istio_mixer_v1_template.TEMPLATE_VARIETY_CHECK,
			CtrCfg:  &types.Empty{},
			InferType: func(p proto.Message, evalFn template.TypeEvalFn) (proto.Message, error) {
				_, _ = evalFn("source.name")
				return &types.Empty{}, nil
			},
			BuilderSupportsTemplate: func(hndlrBuilder adapter.HandlerBuilder) bool {
				return true
			},
			HandlerSupportsTemplate: func(hndlr adapter.Handler) bool {
				if h, ok := hndlr.(*FakeHandler); ok {
					return !h.DoesNotSupportTemplate
				}

				// Always return true otherwise.
				return true
			},
			SetType: func(types map[string]proto.Message, builder adapter.HandlerBuilder) {

			},
			CreateInstanceBuilder: func(instanceName string, instanceParam interface{}, builder *compiled.ExpressionBuilder) template.InstanceBuilderFn {
				return func(bag attribute.Bag) (interface{}, error) {
					return &types.Empty{}, nil
				}
			},
		},

		"t2apa": {
			Name:    "t2apa",
			Variety: istio_mixer_v1_template.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR,
			CtrCfg:  &types.Empty{},
			InferType: func(p proto.Message, evalFn template.TypeEvalFn) (proto.Message, error) {
				_, _ = evalFn("source.name")
				return &types.Empty{}, nil
			},
			BuilderSupportsTemplate: func(hndlrBuilder adapter.HandlerBuilder) bool {
				return true
			},
			HandlerSupportsTemplate: func(hndlr adapter.Handler) bool {
				if h, ok := hndlr.(*FakeHandler); ok {
					return !h.DoesNotSupportTemplate
				}

				// Always return true otherwise.
				return true
			},
			SetType: func(types map[string]proto.Message, builder adapter.HandlerBuilder) {

			},
			CreateInstanceBuilder: func(instanceName string, instanceParam interface{}, builder *compiled.ExpressionBuilder) template.InstanceBuilderFn {
				return func(bag attribute.Bag) (interface{}, error) {
					return &types.Empty{}, nil
				}
			},
			CreateOutputMapperFn: func(instanceParam interface{}, builder *compiled.ExpressionBuilder) template.OutputMapperFn {
				return func(attrs attribute.Bag) (*attribute.MutableBag, error) {
					return attribute.GetMutableBag(attrs), nil
				}
			},
		},
	}

	if override != nil {
		if override.InferType != nil {
			t["t1"].InferType = override.InferType
		}
		if override.BuilderSupportsTemplate != nil {
			t["t1"].BuilderSupportsTemplate = override.BuilderSupportsTemplate
		}
		if override.HandlerSupportsTemplate != nil {
			t["t1"].HandlerSupportsTemplate = override.HandlerSupportsTemplate
		}
	}

	return t
}
