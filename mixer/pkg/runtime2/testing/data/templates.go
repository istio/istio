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
	"context"
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
func BuildTemplates(l *Logger, settings ...FakeTemplateSettings) map[string]*template.Info {
	m := make(map[string]FakeTemplateSettings)
	for _, setting := range settings {
		m[setting.Name] = setting
	}

	var t = map[string]*template.Info{
		"tcheck":  createFakeTemplate("tcheck", m["tcheck"], l, istio_mixer_v1_template.TEMPLATE_VARIETY_CHECK),
		"treport": createFakeTemplate("treport", m["treport"], l, istio_mixer_v1_template.TEMPLATE_VARIETY_REPORT),
		"tquota":  createFakeTemplate("tquota", m["tquota"], l, istio_mixer_v1_template.TEMPLATE_VARIETY_QUOTA),
		"tapa":    createFakeTemplate("tapa", m["tapa"], l, istio_mixer_v1_template.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR),

		// This is another template with check.
		"thalt": createFakeTemplate("thalt", m["thalt"], l, istio_mixer_v1_template.TEMPLATE_VARIETY_CHECK),
	}

	return t
}

func createFakeTemplate(name string, s FakeTemplateSettings, l *Logger, variety istio_mixer_v1_template.TemplateVariety) *template.Info {
	callCount := 0

	return &template.Info{
		Name:    name,
		Variety: variety,
		CtrCfg:  &types.Struct{},
		InferType: func(p proto.Message, evalFn template.TypeEvalFn) (proto.Message, error) {
			l.writeFormat(name, "InferType => p: '%+v'", p)

			if s.ErrorAtInferType {
				l.writeFormat(name, "InferType <= (FAIL)")
				return nil, fmt.Errorf("infer type error, as requested")
			}

			_, _ = evalFn("source.name")
			l.writeFormat(name, "InferType <= (SUCCESS)")
			return &types.Empty{}, nil
		},
		BuilderSupportsTemplate: func(hndlrBuilder adapter.HandlerBuilder) bool {
			l.write(name, "BuilderSupportsTemplate =>")
			l.writeFormat(name, "BuilderSupportsTemplate <= %v", !s.BuilderDoesNotSupportTemplate)
			return !s.BuilderDoesNotSupportTemplate
		},
		HandlerSupportsTemplate: func(hndlr adapter.Handler) bool {
			l.write(name, "HandlerSupportsTemplate =>")
			l.writeFormat(name, "HandlerSupportsTemplate <= %v", !s.HandlerDoesNotSupportTemplate)
			return !s.HandlerDoesNotSupportTemplate
		},
		SetType: func(types map[string]proto.Message, builder adapter.HandlerBuilder) {
			l.writeFormat(name, "SetType => types: '%+v'", types)
			l.write(name, "SetType <=")
		},
		DispatchCheck: func(ctx context.Context, handler adapter.Handler, instance interface{}) (adapter.CheckResult, error) {
			l.writeFormat(name, "DispatchCheck => instance: '%+v'", instance)
			if s.PanicOnDispatchCheck {
				l.write(name, "DispatchCheck <= (PANIC)")
				panic(s.PanicData)
			}

			if s.ErrorOnDispatchCheck {
				l.write(name, "DispatchCheck <= (ERROR)")
				return adapter.CheckResult{}, errors.New("error at dispatch check, as expected")
			}

			result := adapter.CheckResult{}
			if callCount < len(s.CheckResults) {
				result = s.CheckResults[callCount]
			}
			callCount++

			l.write(name, "DispatchCheck <= (SUCCESS)")
			return result, nil
		},
		DispatchReport: func(ctx context.Context, handler adapter.Handler, instances []interface{}) error {
			l.writeFormat(name, "DispatchReport => instances: '%+v'", instances)
			if s.PanicOnDispatchReport {
				l.write(name, "DispatchReport <= (PANIC)")
				panic(s.PanicData)
			}

			if s.ErrorOnDispatchReport {
				l.write(name, "DispatchReport <= (ERROR)")
				return errors.New("error at dispatch report, as expected")
			}

			l.write(name, "DispatchReport <= (SUCCESS)")
			return nil
		},
		DispatchQuota: func(ctx context.Context, handler adapter.Handler, instance interface{}, args adapter.QuotaArgs) (adapter.QuotaResult, error) {
			l.writeFormat(name, "DispatchQuota => instance: '%+v'", instance)
			if s.PanicOnDispatchQuota {
				l.write(name, "DispatchQuota <= (PANIC)")
				panic(s.PanicData)
			}

			if s.ErrorOnDispatchQuota {
				l.write(name, "DispatchQuota <= (ERROR)")
				return adapter.QuotaResult{}, errors.New("error at dispatch quota, as expected")
			}

			result := adapter.QuotaResult{}
			if callCount < len(s.QuotaResults) {
				result = s.QuotaResults[callCount]
			}
			callCount++

			l.write(name, "DispatchQuota <= (SUCCESS)")
			return result, nil
		},
		DispatchGenAttrs: func(ctx context.Context, handler adapter.Handler, instance interface{},
			attrs attribute.Bag, mapper template.OutputMapperFn) (*attribute.MutableBag, error) {
			l.writeFormat(name, "DispatchGenAttrs => instance: '%+v'", instance)
			if s.PanicOnDispatchGenAttrs {
				l.write(name, "DispatchGenAttrs <= (PANIC)")
				panic(s.PanicData)
			}

			if s.ErrorOnDispatchGenAttrs {
				l.write(name, "DispatchGenAttrs <= (ERROR)")
				return nil, errors.New("error at dispatch quota, as expected")
			}

			outputAttrs := map[string]interface{}{}
			if s.OutputAttrs != nil {
				outputAttrs = s.OutputAttrs
			}

			l.write(name, "DispatchGenAttrs <= (SUCCESS)")
			return attribute.GetFakeMutableBagForTesting(outputAttrs), nil

		},
		CreateInstanceBuilder: func(instanceName string, instanceParam proto.Message, builder *compiled.ExpressionBuilder) (template.InstanceBuilderFn, error) {
			l.writeFormat(name, "CreateInstanceBuilder => name: '%+s', param: '%v'", instanceName, instanceParam)
			if s.ErrorAtCreateInstanceBuilder {
				l.writeFormat(name, "CreateInstanceBuilder <= (FAIL)")
				return nil, errors.New("error at create instance builder")
			}

			l.writeFormat(name, "CreateInstanceBuilder <= (SUCCESS)")

			return func(bag attribute.Bag) (interface{}, error) {
				l.writeFormat(name, "InstanceBuilderFn() => name: '%s', bag: '%v'", name, bag.DebugString())

				if s.ErrorAtCreateInstance {
					l.write(name, "InstanceBuilderFn() <= (ERROR)")
					return nil, errors.New("error at create instance")
				}

				l.write(name, "InstanceBuilderFn() <= (SUCCESS)")
				return &types.Empty{}, nil
			}, nil
		},
		CreateOutputExpressions: func(
			instanceParam proto.Message,
			finder expr.AttributeDescriptorFinder,
			expb *compiled.ExpressionBuilder) (map[string]compiled.Expression, error) {
			l.writeFormat(name, "CreateOutputExpressions => param: '%+v'", instanceParam)

			l.writeFormat(name, "CreateOutputExpressions <= (SUCCESS)")
			if s.ErrorAtCreateOutputExpressions {
				l.writeFormat(name, "CreateOutputExpressions <= (FAIL)")
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
	PanicOnDispatchCheck           bool
	ErrorOnDispatchCheck           bool
	PanicOnDispatchReport          bool
	ErrorOnDispatchReport          bool
	PanicOnDispatchQuota           bool
	ErrorOnDispatchQuota           bool
	PanicOnDispatchGenAttrs        bool
	ErrorOnDispatchGenAttrs        bool
	PanicData                      interface{}
	QuotaResults                   []adapter.QuotaResult
	CheckResults                   []adapter.CheckResult
	OutputAttrs                    map[string]interface{}
}
