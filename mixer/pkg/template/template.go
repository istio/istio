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
	"github.com/golang/protobuf/proto"

	pb "istio.io/api/mixer/v1/config/descriptor"
	adptConfig "istio.io/mixer/pkg/adapter/config"
)

type (
	// Repository defines all the helper functions to access the generated template specific types and fields.
	Repository interface {
		GetTemplateInfo(template string) (Info, bool)
	}
	// TypeEvalFn evaluates an expression and returns the ValueType for the expression.
	TypeEvalFn func(string) (pb.ValueType, error)
	// InferTypeFn does Type inference from the Constructor.params proto message.
	InferTypeFn func(proto.Message, TypeEvalFn) (proto.Message, error)
	// ConfigureTypeFn dispatches the inferred types to handlers
	ConfigureTypeFn func(types map[string]proto.Message, builder *adptConfig.HandlerBuilder) error
	// Info contains all the information related a template like
	// Default constructor params, type inference method etc.
	Info struct {
		CnstrDefConfig  proto.Message
		InferTypeFn     InferTypeFn
		ConfigureTypeFn ConfigureTypeFn
	}
	// templateRepo implements Repository
	templateRepo struct{}
)

func (t templateRepo) GetTemplateInfo(template string) (Info, bool) {
	if v, ok := templateInfos[template]; ok {
		return v, true
	}
	return Info{}, false
}

// NewTemplateRepository returns an implementation of Repository
func NewTemplateRepository() Repository {
	return templateRepo{}
}
