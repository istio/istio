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

package template

import (
	"context"
	"fmt"
	"net"

	"github.com/gogo/protobuf/proto"
	multierror "github.com/hashicorp/go-multierror"

	"istio.io/api/mixer/v1/config"
	pb "istio.io/api/mixer/v1/config/descriptor"
	adptTmpl "istio.io/api/mixer/v1/template"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/expr"
	"istio.io/istio/mixer/pkg/il/compiled"
	"istio.io/istio/pkg/log"
)

type (
	// Repository defines all the helper functions to access the generated template specific types and fields.
	Repository interface {
		GetTemplateInfo(template string) (Info, bool)
		SupportsTemplate(hndlrBuilder adapter.HandlerBuilder, tmpl string) (bool, string)
	}
	// TypeEvalFn evaluates an expression and returns the ValueType for the expression.
	TypeEvalFn func(string) (pb.ValueType, error)
	// InferTypeFn does Type inference from the Instance.params proto message.
	InferTypeFn func(proto.Message, TypeEvalFn) (proto.Message, error)
	// SetTypeFn dispatches the inferred types to handlers
	SetTypeFn func(types map[string]proto.Message, builder adapter.HandlerBuilder)

	// ProcessCheckFn instantiates the instance object and dispatches them to the handler.
	ProcessCheckFn func(ctx context.Context, instName string, instCfg proto.Message, attrs attribute.Bag,
		mapper expr.Evaluator, handler adapter.Handler) (adapter.CheckResult, error)

	// ProcessQuotaFn instantiates the instance object and dispatches them to the handler.
	ProcessQuotaFn func(ctx context.Context, quotaName string, quotaCfg proto.Message, attrs attribute.Bag,
		mapper expr.Evaluator, handler adapter.Handler, args adapter.QuotaArgs) (adapter.QuotaResult, error)

	// ProcessReportFn instantiates the instance object and dispatches them to the handler.
	ProcessReportFn func(ctx context.Context, instCfg map[string]proto.Message, attrs attribute.Bag,
		mapper expr.Evaluator, handler adapter.Handler) error

	// ProcessGenerateAttributesFn instantiates the instance object and dispatches them to the attribute generating handler.
	ProcessGenerateAttributesFn func(ctx context.Context, instName string, instCfg proto.Message, attrs attribute.Bag,
		mapper expr.Evaluator, handler adapter.Handler) (*attribute.MutableBag, error)

	// BuilderSupportsTemplateFn check if the handlerBuilder supports template.
	BuilderSupportsTemplateFn func(hndlrBuilder adapter.HandlerBuilder) bool

	// HandlerSupportsTemplateFn check if the handler supports template.
	HandlerSupportsTemplateFn func(hndlr adapter.Handler) bool

	// DispatchCheckFn dispatches the instance to the handler.
	DispatchCheckFn func(ctx context.Context, handler adapter.Handler, instance interface{}) (adapter.CheckResult, error)

	// DispatchReportFn dispatches the instances to the handler.
	DispatchReportFn func(ctx context.Context, handler adapter.Handler, instances []interface{}) error

	// DispatchQuotaFn dispatches the instance to the handler.
	DispatchQuotaFn func(ctx context.Context, handler adapter.Handler, instance interface{}, args adapter.QuotaArgs) (adapter.QuotaResult, error)

	// DispatchGenerateAttributesFn dispatches the instance object to the attribute generating handler.
	DispatchGenerateAttributesFn func(ctx context.Context, handler adapter.Handler, instance interface{},
		attrs attribute.Bag, mapper OutputMapperFn) (*attribute.MutableBag, error)

	// InstanceBuilderFn builds and returns an instance, based on the attributes supplied.
	// Typically, InstanceBuilderFn closes over an auto-generated "builder" struct which contains compiled
	// expressions and sub-builders for the instance:
	//
	//  // An InstanceBuilderFn implementation:
	//  func(attrs attribute.Bag) (interface{}, error) {
	//    // myInstanceBuilder is an instance of a builder struct that this function closes over (see below).
	//    return myInstanceBuilder.build(bag)
	//  }
	//
	//  // myInstanceBuilder is an auto-generated struct for building instances. They get instantiated
	//  // based on instance parameters (see CreateInstanceBuilderFn documentation):
	//  type myInstanceBuilder struct {
	//
	//     // field1Builder builds the value of the field field1 of the instance.
	//     field1Builder compiled.Expression
	//     ...
	//  }
	//
	//  func (b *myInstanceBuilder) build(attrs attribute.Bag) (*myInstance, error) {
	//    ...
	//    instance := &myInstance{}
	//
	//    // Build the value of field1
	//    if instance1.field1, err = b.field1Builder.EvaluateString(bag); err != nil {
	//       return nil, err
	//    }
	//    ...
	//    return instance, nil
	//
	InstanceBuilderFn func(attrs attribute.Bag) (interface{}, error)

	// CreateInstanceBuilderFn returns a function that can build instances, based on the instanceParam:
	//
	//  // A CreateInstanceBuilderFn implementation:
	//  func(instanceName string, instanceParam interface{}, builder *compiled.ExpressionBuilder) (InstanceBuilderFn, error) {
	//    // Call an auto-generated function to create a builder struct.
	//    builder, err := newMyInstanceBuilder(instanceName, instanceParam, builder)
	//    if err != nil {
	//       return nil, err
	//    }
	//
	//    // return an InstanceBuilderfn
	//    func(attrs attribute.Bag) (interface{}, error) {
	//      // myInstanceBuilder is an instance of a builder struct that this function closes over (see below).
	//      return myInstanceBuilder.build(bag)
	//    }
	//  }
	//
	//  // Auto-generated method for creating a new builder struct
	//  func newMyInstanceBuilder(instanceName string, instanceParam interface{}, exprBuilder *compiled.ExpressionBuilder) (*myInstanceBuilder, error) {
	//     myInstanceParam := instanceParam.(*myInstanceParam)
	//     builder := &myInstanceBuilder{}
	//
	//     builder.field1Builder, err = exprBuilder.Compile(myInstanceParam.field1Expression)
	//     if err != nil {
	//       return nil, err
	//     }
	//  }
	CreateInstanceBuilderFn func(instanceName string, instanceParam proto.Message, builder *compiled.ExpressionBuilder) (InstanceBuilderFn, error)

	// CreateOutputExpressionsFn builds and returns a map of attribute names to the expression for calculating them.
	//
	//  // A CreateOutputExpressionsFn implementation:
	//  func(instanceParam interface{}, finder expr.AttributeDescriptorFinder, builder *compiled.ExpressionBuilder) (map[string]compiled.Expression, error) {
	//
	//    // Convert the generic instanceParam to its specialized type.
	//     param := instanceParam.(*myInstanceParam)
	//
	//    // Create a mapping of expressions back to the attribute names.
	//    expressions := make(map[string]compiled.Expression, len(param.AttributeBindings))
	//    for attrName, outExpr := range param.AttributeBindings {
	//       // compile an expression and put it in expressions.
	//    }
	//
	//    return expressions, nil
	//  }
	//
	CreateOutputExpressionsFn func(
		instanceParam proto.Message,
		finder expr.AttributeDescriptorFinder,
		expb *compiled.ExpressionBuilder) (map[string]compiled.Expression, error)

	// OutputMapperFn maps the results of an APA output bag, with "$out"s, by processing it through
	// AttributeBindings.
	//
	//  MapperFn: func(attrs attribute.Bag) (*attribute.MutableBag, error) {...}
	//
	//  ProcessGenerateAttributes2Fn(..., attrs attribute.Bag) (*attribute.MutableBag, error) [
	//    // use instance to call into the handler and get back a bag with "$out" values in it.
	//    outBag := ...
	//
	//    // Call an OutputMapperFn to map "$out.<name>" parameters to actual attribute values
	//    resultBag := MapperFn(outBag)
	//    return resultBag, nil
	//  }
	OutputMapperFn func(attrs attribute.Bag) (*attribute.MutableBag, error)

	// Info contains all the information related a template like
	// Default instance params, type inference method etc.
	Info struct {
		Name                    string
		Impl                    string
		Variety                 adptTmpl.TemplateVariety
		BldrInterfaceName       string
		HndlrInterfaceName      string
		CtrCfg                  proto.Message
		InferType               InferTypeFn
		SetType                 SetTypeFn
		BuilderSupportsTemplate BuilderSupportsTemplateFn
		HandlerSupportsTemplate HandlerSupportsTemplateFn
		ProcessReport           ProcessReportFn
		ProcessCheck            ProcessCheckFn
		ProcessQuota            ProcessQuotaFn
		ProcessGenAttrs         ProcessGenerateAttributesFn

		AttributeManifests []*config.AttributeManifest

		DispatchReport   DispatchReportFn
		DispatchCheck    DispatchCheckFn
		DispatchQuota    DispatchQuotaFn
		DispatchGenAttrs DispatchGenerateAttributesFn

		CreateInstanceBuilder   CreateInstanceBuilderFn
		CreateOutputExpressions CreateOutputExpressionsFn
	}

	// templateRepo implements Repository
	repo struct {
		info map[string]Info

		allSupportedTmpls  []string
		tmplToBuilderNames map[string]string
	}
)

func (t repo) GetTemplateInfo(template string) (Info, bool) {
	if v, ok := t.info[template]; ok {
		return v, true
	}
	return Info{}, false
}

// NewRepository returns an implementation of Repository
func NewRepository(templateInfos map[string]Info) Repository {
	if templateInfos == nil {
		return repo{
			info:               make(map[string]Info),
			allSupportedTmpls:  make([]string, 0),
			tmplToBuilderNames: make(map[string]string),
		}
	}

	allSupportedTmpls := make([]string, len(templateInfos))
	tmplToBuilderNames := make(map[string]string)

	for t, v := range templateInfos {
		allSupportedTmpls = append(allSupportedTmpls, t)
		tmplToBuilderNames[t] = v.BldrInterfaceName
	}
	return repo{info: templateInfos, tmplToBuilderNames: tmplToBuilderNames, allSupportedTmpls: allSupportedTmpls}
}

func (t repo) SupportsTemplate(hndlrBuilder adapter.HandlerBuilder, tmpl string) (bool, string) {
	i, ok := t.GetTemplateInfo(tmpl)
	if !ok {
		return false, fmt.Sprintf("Supported template %v is not one of the allowed supported templates %v", tmpl, t.allSupportedTmpls)
	}

	if b := i.BuilderSupportsTemplate(hndlrBuilder); !b {
		return false, fmt.Sprintf("HandlerBuilder does not implement interface %s. "+
			"Therefore, it cannot support template %v", t.tmplToBuilderNames[tmpl], tmpl)
	}

	return true, ""
}

// EvalAll evaluates the value of the expression map using the passed in attributes.
func EvalAll(expressions map[string]string, attrs attribute.Bag, eval expr.Evaluator) (map[string]interface{}, error) {
	result := &multierror.Error{}
	labels := make(map[string]interface{}, len(expressions))
	for label, texpr := range expressions {
		val, err := eval.Eval(texpr, attrs)
		if err != nil {
			result = multierror.Append(result, fmt.Errorf("failed to construct value for label '%s': %v", label, err))
			continue
		}
		labels[label] = val
	}
	return labels, result.ErrorOrNil()
}

// NewOutputMapperFn creates and returns a function that creates new attributes, based on the supplied expression set.
func NewOutputMapperFn(expressions map[string]compiled.Expression) OutputMapperFn {
	return func(attrs attribute.Bag) (*attribute.MutableBag, error) {
		var val interface{}
		var err error

		resultBag := attribute.GetMutableBag(nil)
		for attrName, expr := range expressions {
			if val, err = expr.Evaluate(attrs); err != nil {
				return nil, err
			}

			switch v := val.(type) {
			case net.IP:
				// conversion to []byte necessary based on current IP_ADDRESS handling within Mixer
				// TODO: remove
				log.Info("converting net.IP to []byte")
				if v4 := v.To4(); v4 != nil {
					resultBag.Set(attrName, []byte(v4))
					continue
				}
				resultBag.Set(attrName, []byte(v.To16()))
			default:
				resultBag.Set(attrName, val)
			}
		}

		return resultBag, nil
	}
}
