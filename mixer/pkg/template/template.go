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
	"istio.io/istio/mixer/pkg/il/compiled"
	"istio.io/istio/pkg/log"

	pb "istio.io/api/mixer/v1/config/descriptor"
	adptTmpl "istio.io/api/mixer/v1/template"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/config/proto"
	"istio.io/istio/mixer/pkg/expr"
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

		AttributeManifests []*istio_mixer_v1_config.AttributeManifest

		ProcessReport2   ProcessReport2Fn
		ProcessCheck2    ProcessCheck2Fn
		ProcessQuota2    ProcessQuota2Fn
		ProcessGenAttrs2 ProcessGenAttrs2Fn

		CreateInstanceBuilder CreateInstanceBuilderFn
		CreateOutputMapperFn  CreateOutputMapperFn
	}

	// templateRepo implements Repository
	repo struct {
		info map[string]Info

		allSupportedTmpls  []string
		tmplToBuilderNames map[string]string
	}

	ProcessReport2Fn   func(ctx context.Context, handler adapter.Handler, instances []interface{}) error
	ProcessCheck2Fn    func(ctx context.Context, handler adapter.Handler, instance interface{}) (adapter.CheckResult, error)
	ProcessQuota2Fn    func(ctx context.Context, handler adapter.Handler, instance interface{}, args adapter.QuotaArgs) (adapter.QuotaResult, error)
	ProcessGenAttrs2Fn func(ctx context.Context, handler adapter.Handler, instance interface{},
		attrs attribute.Bag, mapper OutputMapperFn) (*attribute.MutableBag, error)

	// CreateInstanceBuilderFn builds and returns a function that will build Instances during runtime.
	CreateInstanceBuilderFn func(instanceName string, instanceParam interface{}, builder *compiled.ExpressionBuilder) InstanceBuilderFn

	// InstanceBuilderFn builds returns an Instance, based on the attributes supplied.
	// It closes over the current configuration and the instanceParam supplied during
	// its creation.
	InstanceBuilderFn func(attrs attribute.Bag) (interface{}, error)

	// CreateOutputMapperFn builds and returns a function that will map APA output values to attributes.
	CreateOutputMapperFn func(instanceParam interface{}, builder *compiled.ExpressionBuilder) OutputMapperFn

	// OutputMapperFn maps the results of an APA output bag (with $out)s by processing it through
	// AttributeBindings
	// It closes over the current configuration and the instanceParam supplied during
	// its creation.
	OutputMapperFn func(attrs attribute.Bag) (*attribute.MutableBag, error)
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
