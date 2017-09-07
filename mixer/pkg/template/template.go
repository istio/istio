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

	"github.com/gogo/protobuf/proto"
	multierror "github.com/hashicorp/go-multierror"

	pb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/adapter"
	adptTmpl "istio.io/mixer/pkg/adapter/template"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"
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
		mapper expr.Evaluator, handler adapter.Handler, args adapter.QuotaRequestArgs) (adapter.QuotaResult2, error)

	// ProcessReportFn instantiates the instance object and dispatches them to the handler.
	ProcessReportFn func(ctx context.Context, instCfg map[string]proto.Message, attrs attribute.Bag,
		mapper expr.Evaluator, handler adapter.Handler) error

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
