// Copyright 2018 Istio Authors
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
	"errors"
	"fmt"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	istio_mixer_v1_template "istio.io/api/mixer/v1/template"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/expr"
	"istio.io/istio/mixer/pkg/il/compiled"
	"istio.io/istio/mixer/pkg/template"
)

// BuildTemplates builds a standard set of testing templates. The supplied settings is used to override behavior.
func BuildTemplates(settings ...FakeTemplateSettings) map[string]*template.Info {
	m := make(map[string]FakeTemplateSettings)
	for _, setting := range settings {
		m[setting.Name] = setting
	}

	var t = map[string]*template.Info{
		"tcheck": createFakeTemplate("tcheck", m["tcheck"], istio_mixer_v1_template.TEMPLATE_VARIETY_CHECK),
		"tapa":   createFakeTemplate("tapa", m["tapa"], istio_mixer_v1_template.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR),

		// This is another template with check.
		"thalt": createFakeTemplate("thalt", m["thalt"], istio_mixer_v1_template.TEMPLATE_VARIETY_CHECK),
	}

	return t
}

func createFakeTemplate(name string, s FakeTemplateSettings, variety istio_mixer_v1_template.TemplateVariety) *template.Info {
	return &template.Info{
		Name:    name,
		Variety: variety,
		CtrCfg:  &types.Struct{},
		InferType: func(p proto.Message, evalFn template.TypeEvalFn) (proto.Message, error) {
			if s.ErrorAtInferType {
				return nil, fmt.Errorf("infer type error, as requested")
			}

			_, _ = evalFn("source.name")
			return &types.Empty{}, nil
		},
		BuilderSupportsTemplate: func(hndlrBuilder adapter.HandlerBuilder) bool {
			return !s.BuilderDoesNotSupportTemplate
		},
		HandlerSupportsTemplate: func(hndlr adapter.Handler) bool {
			return !s.HandlerDoesNotSupportTemplate
		},
		SetType: func(types map[string]proto.Message, builder adapter.HandlerBuilder) {

		},
		CreateInstanceBuilder: func(instanceName string, instanceParam proto.Message, builder *compiled.ExpressionBuilder) (template.InstanceBuilderFn, error) {
			if s.ErrorAtCreateInstanceBuilder {
				return nil, errors.New("error at create instance builder")
			}

			return func(bag attribute.Bag) (interface{}, error) {

				if s.ErrorAtCreateInstance {
					return nil, errors.New("error at create instance")
				}
				return &types.Empty{}, nil
			}, nil
		},
		CreateOutputExpressions: func(
			instanceParam proto.Message,
			finder expr.AttributeDescriptorFinder,
			expb *compiled.ExpressionBuilder) (map[string]compiled.Expression, error) {

			if s.ErrorAtCreateOutputExpressions {
				return nil, errors.New("error ar create output expressions")
			}

			return make(map[string]compiled.Expression), nil
		},
	}
}

// FakeTemplateSettings describes the behavior of a fake template.
type FakeTemplateSettings struct {
	Name                           string
	ErrorAtCreateInstance          bool
	ErrorAtCreateInstanceBuilder   bool
	ErrorAtCreateOutputExpressions bool
	ErrorAtInferType               bool
	BuilderDoesNotSupportTemplate  bool
	HandlerDoesNotSupportTemplate  bool
}
