// Copyright Istio Authors
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
	"strings"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	istio_mixer_v1_template "istio.io/api/mixer/adapter/model/v1beta1"
	policy "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/lang/compiled"
	"istio.io/istio/mixer/pkg/runtime/lang"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/pkg/attribute"
)

// BuildTemplates builds a standard set of testing templates. The supplied settings is used to override behavior.
func BuildTemplates(l *Logger, settings ...FakeTemplateSettings) map[string]*template.Info {
	m := make(map[string]FakeTemplateSettings)
	for _, setting := range settings {
		m[setting.Name] = setting
	}

	var t = map[string]*template.Info{
		"tcheck":       createFakeTemplate("tcheck", m["tcheck"], l, istio_mixer_v1_template.TEMPLATE_VARIETY_CHECK),
		"tcheckoutput": createFakeTemplate("tcheckoutput", m["tcheckoutput"], l, istio_mixer_v1_template.TEMPLATE_VARIETY_CHECK_WITH_OUTPUT),
		"treport":      createFakeTemplate("treport", m["treport"], l, istio_mixer_v1_template.TEMPLATE_VARIETY_REPORT),
		"tquota":       createFakeTemplate("tquota", m["tquota"], l, istio_mixer_v1_template.TEMPLATE_VARIETY_QUOTA),
		"tapa":         createFakeTemplate("tapa", m["tapa"], l, istio_mixer_v1_template.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR),

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
		AttributeManifests: []*policy.AttributeManifest{
			{
				Attributes: map[string]*policy.AttributeManifest_AttributeInfo{
					"prefix.generated.string": {
						ValueType: policy.STRING,
					},
				},
			},
			// output template attribute manifest
			{
				Attributes: map[string]*policy.AttributeManifest_AttributeInfo{
					"value": {
						ValueType: policy.STRING,
					},
				},
			},
		},
		InferType: func(p proto.Message, evalFn template.TypeEvalFn) (proto.Message, error) {
			l.WriteFormat(name, "InferType => p: '%+v'", p)

			if s.ErrorAtInferType {
				l.WriteFormat(name, "InferType <= (FAIL)")
				return nil, fmt.Errorf("infer type error, as requested")
			}

			_, _ = evalFn("source.name")
			l.WriteFormat(name, "InferType <= (SUCCESS)")
			return &types.Struct{}, nil
		},
		BuilderSupportsTemplate: func(hndlrBuilder adapter.HandlerBuilder) bool {
			l.Write(name, "BuilderSupportsTemplate =>")
			l.WriteFormat(name, "BuilderSupportsTemplate <= %v", !s.BuilderDoesNotSupportTemplate)
			return !s.BuilderDoesNotSupportTemplate
		},
		HandlerSupportsTemplate: func(hndlr adapter.Handler) bool {
			l.Write(name, "HandlerSupportsTemplate =>")
			l.WriteFormat(name, "HandlerSupportsTemplate <= %v", !s.HandlerDoesNotSupportTemplate)
			return !s.HandlerDoesNotSupportTemplate
		},
		SetType: func(types map[string]proto.Message, builder adapter.HandlerBuilder) {
			l.WriteFormat(name, "SetType => types: '%+v'", types)
			l.Write(name, "SetType <=")
		},
		DispatchCheck: func(ctx context.Context, handler adapter.Handler, instance interface{}, out *attribute.MutableBag,
			outPrefix string) (adapter.CheckResult, error) {
			l.WriteFormat(name, "DispatchCheck => context exists: '%+v'", ctx != nil)
			l.WriteFormat(name, "DispatchCheck => handler exists: '%+v'", handler != nil)
			l.WriteFormat(name, "DispatchCheck => instance:       '%+v'", instance)

			signalCallAndWait(s)

			if s.PanicOnDispatchCheck {
				l.Write(name, "DispatchCheck <= (PANIC)")
				panic(s.PanicData)
			}

			if s.ErrorOnDispatchCheck {
				l.Write(name, "DispatchCheck <= (ERROR)")
				return adapter.CheckResult{}, errors.New("error at dispatch check, as expected")
			}

			result := adapter.CheckResult{
				ValidUseCount: 123,
				ValidDuration: 123 * time.Second,
			}
			if callCount < len(s.CheckResults) {
				result = s.CheckResults[callCount]
			}
			callCount++

			l.Write(name, "DispatchCheck <= (SUCCESS)")

			if variety == istio_mixer_v1_template.TEMPLATE_VARIETY_CHECK_WITH_OUTPUT {
				l.Write(name, "DispatchCheck => output: {value: '1337'}")
				if out != nil {
					out.Set(outPrefix+"value", "1337")
				}
				return result, nil
			}
			return result, nil
		},
		DispatchReport: func(ctx context.Context, handler adapter.Handler, instances []interface{}) error {
			l.WriteFormat(name, "DispatchReport => context exists: '%+v'", ctx != nil)
			l.WriteFormat(name, "DispatchReport => handler exists: '%+v'", handler != nil)
			l.WriteFormat(name, "DispatchReport => instances: '%+v'", instances)

			signalCallAndWait(s)

			if s.PanicOnDispatchReport {
				l.Write(name, "DispatchReport <= (PANIC)")
				panic(s.PanicData)
			}

			if s.ErrorOnDispatchReport {
				l.Write(name, "DispatchReport <= (ERROR)")
				return errors.New("error at dispatch report, as expected")
			}

			l.Write(name, "DispatchReport <= (SUCCESS)")
			return nil
		},
		DispatchQuota: func(ctx context.Context, handler adapter.Handler, instance interface{}, args adapter.QuotaArgs) (adapter.QuotaResult, error) {
			l.WriteFormat(name, "DispatchQuota => context exists: '%+v'", ctx != nil)
			l.WriteFormat(name, "DispatchQuota => handler exists: '%+v'", handler != nil)
			l.WriteFormat(name, "DispatchQuota => instance: '%+v' qArgs:{dedup:'%v', amount:'%v', best:'%v'}",
				instance, args.DeduplicationID, args.QuotaAmount, args.BestEffort)

			signalCallAndWait(s)

			if s.PanicOnDispatchQuota {
				l.Write(name, "DispatchQuota <= (PANIC)")
				panic(s.PanicData)
			}

			if s.ErrorOnDispatchQuota {
				l.Write(name, "DispatchQuota <= (ERROR)")
				return adapter.QuotaResult{}, errors.New("error at dispatch quota, as expected")
			}

			result := adapter.QuotaResult{}
			if callCount < len(s.QuotaResults) {
				result = s.QuotaResults[callCount]
			}
			callCount++

			l.Write(name, "DispatchQuota <= (SUCCESS)")
			return result, nil
		},
		DispatchGenAttrs: func(ctx context.Context, handler adapter.Handler, instance interface{},
			attrs attribute.Bag, mapper template.OutputMapperFn) (*attribute.MutableBag, error) {
			l.WriteFormat(name, "DispatchGenAttrs => instance: '%+v'", instance)
			l.WriteFormat(name, "DispatchGenAttrs => attrs:    '%+v'", attrs.String())
			l.WriteFormat(name, "DispatchGenAttrs => mapper(exists):   '%+v'", mapper != nil)

			signalCallAndWait(s)

			if s.PanicOnDispatchGenAttrs {
				l.Write(name, "DispatchGenAttrs <= (PANIC)")
				panic(s.PanicData)
			}

			if s.ErrorOnDispatchGenAttrs {
				l.Write(name, "DispatchGenAttrs <= (ERROR)")
				return nil, errors.New("error at dispatch quota, as expected")
			}

			outputAttrs := map[string]interface{}{}
			if s.OutputAttrs != nil {
				outputAttrs = s.OutputAttrs
			}

			l.Write(name, "DispatchGenAttrs <= (SUCCESS)")
			return attribute.GetMutableBagForTesting(outputAttrs), nil

		},
		CreateInstanceBuilder: func(instanceName string, instanceParam proto.Message, builder lang.Compiler) (template.InstanceBuilderFn, error) {
			l.WriteFormat(name, "CreateInstanceBuilder => instanceName: '%+s'", instanceName)
			l.WriteFormat(name, "CreateInstanceBuilder => instanceParam: '%s'", instanceParam)
			if s.ErrorAtCreateInstanceBuilder {
				l.WriteFormat(name, "CreateInstanceBuilder <= (FAIL)")
				return nil, errors.New("error at create instance builder")
			}

			l.WriteFormat(name, "CreateInstanceBuilder <= (SUCCESS)")

			ip := instanceParam.(*types.Struct)
			exprs := make(map[string]compiled.Expression)
			for k, v := range ip.Fields {
				if k == "attribute_bindings" {
					continue
				}

				exp, _, err := builder.Compile(v.GetStringValue())
				if err != nil {
					l.WriteFormat(name, "InstanceBuilderFn() <= (UNEXPECTED ERROR) (%v) %v", v.GetStringValue(), err)
					return nil, fmt.Errorf("error compiling expression: %v=%v => %v", k, v, err)
				}
				exprs[k] = exp
			}

			return func(bag attribute.Bag) (interface{}, error) {
				l.WriteFormat(name, "InstanceBuilderFn() => name: '%s', bag: '%v'", name, bag.String())

				if s.ErrorAtCreateInstance {
					l.Write(name, "InstanceBuilderFn() <= (ERROR)")
					return nil, errors.New("error at create instance")
				}

				l.Write(name, "InstanceBuilderFn() <= (SUCCESS)")

				instance := &types.Struct{
					Fields: make(map[string]*types.Value),
				}
				for k, exp := range exprs {
					v, err := exp.Evaluate(bag)
					if err != nil {
						return nil, err
					}

					instance.Fields[k] = &types.Value{
						Kind: &types.Value_StringValue{StringValue: fmt.Sprintf("%v", v)},
					}
				}
				return instance, nil
			}, nil
		},
		CreateOutputExpressions: func(
			instanceParam proto.Message,
			finder attribute.AttributeDescriptorFinder,
			expb lang.Compiler) (map[string]compiled.Expression, error) {
			l.WriteFormat(name, "CreateOutputExpressions => param:            '%+v'", instanceParam)
			l.WriteFormat(name, "CreateOutputExpressions => finder exists:    '%+v'", finder != nil)
			l.WriteFormat(name, "CreateOutputExpressions => expb exists:      '%+v'", expb != nil)

			if s.ErrorAtCreateOutputExpressions {
				l.WriteFormat(name, "CreateOutputExpressions <= (FAIL)")
				return nil, errors.New("error ar create output expressions")
			}

			ip := instanceParam.(*types.Struct)
			exprs := make(map[string]compiled.Expression)
			if bindings, ok := ip.Fields["attribute_bindings"]; ok {
				for k, v := range bindings.GetStructValue().Fields {
					str := strings.Replace(v.GetStringValue(), "$out", "prefix", -1)
					exp, _, err := expb.Compile(str)
					if err != nil {
						l.WriteFormat(name, "CreateOutputExpressions() <= (UNEXPECTED ERROR) (%v) %v", str, err)
						return nil, fmt.Errorf("error compiling expression: %v=%v => %v", k, v, err)
					}
					exprs[k] = exp
				}
			}

			l.Write(name, "CreateOutputExpressions <= (SUCCESS)")
			return exprs, nil
		},
	}
}

func signalCallAndWait(s FakeTemplateSettings) {
	if s.ReceivedCallChannel != nil {
		s.ReceivedCallChannel <- struct{}{}
	}

	if s.CommenceSignalChannel != nil {
		<-s.CommenceSignalChannel
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

	// template will signal the receipt of an incoming dispatch on this channel
	ReceivedCallChannel chan struct{}

	// template will wait on this channel before completing the dispatch
	CommenceSignalChannel chan struct{}
}
