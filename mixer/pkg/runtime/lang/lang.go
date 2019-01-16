// Copyright 2019 Istio Authors
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

// Package lang chooses a language runtime for expressions.
package lang

import (
	"fmt"
	"reflect"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/lang/ast"
	"istio.io/istio/mixer/pkg/lang/cel"
	"istio.io/istio/mixer/pkg/lang/compiled"
)

type (
	// Compiler creates a compiled expression from a string expression
	Compiler interface {
		// Compile creates a compiled expression from a string expression
		Compile(expr string) (compiled.Expression, v1beta1.ValueType, error)
	}
)

// NewBuilder returns an expression builder
func NewBuilder(finder ast.AttributeDescriptorFinder) Compiler {
	return &validatingCompiler{
		cexl: compiled.NewBuilder(finder),
		cel:  cel.NewBuilder(finder, cel.LegacySyntaxCEL),
	}
}

type validatingCompiler struct {
	cel, cexl Compiler
}

type validatingExpression struct {
	expr      string
	cel, cexl compiled.Expression
}

func (vc *validatingCompiler) Compile(expr string) (compiled.Expression, v1beta1.ValueType, error) {
	celEx, celTyp, celErr := vc.cel.Compile(expr)
	cexlEx, cexlTyp, cexlErr := vc.cexl.Compile(expr)

	if cexlErr != nil && celErr == nil {
		panic(fmt.Sprintf("expected CEL error: %v %q", cexlErr, expr))
	}
	if celErr != nil && cexlErr == nil {
		panic(fmt.Sprintf("expected CEXL error: %v %q", celErr, expr))
	}
	if celTyp != cexlTyp {
		panic(fmt.Errorf("type difference CEL %v and CEXL %v for %q", celTyp, cexlTyp, expr))
	}

	return &validatingExpression{expr: expr, cel: celEx, cexl: cexlEx}, celTyp, celErr
}

func (ve *validatingExpression) validate(celEx interface{}, celErr error, cexlEx interface{}, cexlErr error,
	attributes attribute.Bag) {
	if cexlErr != nil && celErr == nil {
		panic(fmt.Sprintf("expected CEL error: %v %q %s", cexlErr, ve.expr, attributes))
	}
	if celErr != nil && cexlErr == nil {
		panic(fmt.Sprintf("expected CEXL error: %v %q %s", celErr, ve.expr, attributes))
	}
	if !reflect.DeepEqual(celEx, cexlEx) {
		panic(fmt.Sprintf("unexpected expression value: CEL %v and CEXL %v, %q %s \n %s", celEx, cexlEx, ve.expr, ve.cel, attributes))
	}
}

func (ve *validatingExpression) Evaluate(attributes attribute.Bag) (interface{}, error) {
	cel, celErr := ve.cel.Evaluate(attributes)
	cexl, cexlErr := ve.cexl.Evaluate(attributes)
	ve.validate(cel, celErr, cexl, cexlErr, attributes)
	return cel, celErr
}
func (ve *validatingExpression) EvaluateBoolean(attributes attribute.Bag) (bool, error) {
	cel, celErr := ve.cel.EvaluateBoolean(attributes)
	cexl, cexlErr := ve.cexl.EvaluateBoolean(attributes)
	ve.validate(cel, celErr, cexl, cexlErr, attributes)
	return cel, celErr
}
func (ve *validatingExpression) EvaluateString(attributes attribute.Bag) (string, error) {
	cel, celErr := ve.cel.EvaluateString(attributes)
	cexl, cexlErr := ve.cexl.EvaluateString(attributes)
	ve.validate(cel, celErr, cexl, cexlErr, attributes)
	return cel, celErr
}
func (ve *validatingExpression) EvaluateDouble(attributes attribute.Bag) (float64, error) {
	cel, celErr := ve.cel.EvaluateDouble(attributes)
	cexl, cexlErr := ve.cexl.EvaluateDouble(attributes)
	ve.validate(cel, celErr, cexl, cexlErr, attributes)
	return cel, celErr
}
func (ve *validatingExpression) EvaluateInteger(attributes attribute.Bag) (int64, error) {
	cel, celErr := ve.cel.EvaluateInteger(attributes)
	cexl, cexlErr := ve.cexl.EvaluateInteger(attributes)
	ve.validate(cel, celErr, cexl, cexlErr, attributes)
	return cel, celErr
}
