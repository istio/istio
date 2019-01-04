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

package cel

import (
	"fmt"

	"github.com/google/cel-go/checker"
	"github.com/google/cel-go/common/debug"
	"github.com/google/cel-go/interpreter"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	descriptor "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/lang/ast"
	"istio.io/istio/mixer/pkg/lang/compiled"
)

// ExpressionBuilder creates a CEL interpreter from an attribute manifest.
type ExpressionBuilder struct {
	provider    *attributeProvider
	env         *checker.Env
	interpreter interpreter.Interpreter
}

type expression struct {
	provider *attributeProvider
	expr     *exprpb.Expr
	eval     interpreter.Interpretable
}

func (ex *expression) Evaluate(bag attribute.Bag) (out interface{}, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic during evaluation: %v", r)
		}
	}()

	result, _ := ex.eval.Eval(ex.provider.newActivation(bag))
	out, err = recoverValue(result)
	return
}
func (ex *expression) EvaluateBoolean(bag attribute.Bag) (b bool, err error) {
	var out interface{}
	out, err = ex.Evaluate(bag)
	b, _ = out.(bool)
	return
}
func (ex *expression) EvaluateString(bag attribute.Bag) (s string, err error) {
	var out interface{}
	out, err = ex.Evaluate(bag)
	s, _ = out.(string)
	return
}
func (ex *expression) EvaluateInteger(bag attribute.Bag) (i int64, err error) {
	var out interface{}
	out, err = ex.Evaluate(bag)
	i, _ = out.(int64)
	return
}
func (ex *expression) EvaluateDouble(bag attribute.Bag) (d float64, err error) {
	var out interface{}
	out, err = ex.Evaluate(bag)
	d, _ = out.(float64)
	return
}
func (ex *expression) String() string {
	return debug.ToDebugString(ex.expr)
}

// NewBuilder returns a new ExpressionBuilder
func NewBuilder(finder ast.AttributeDescriptorFinder) *ExpressionBuilder {
	provider := newAttributeProvider(finder.Attributes())
	return &ExpressionBuilder{
		provider:    provider,
		env:         provider.newEnvironment(),
		interpreter: provider.newInterpreter(),
	}
}

// Compile the given text and return a pre-compiled expression object.
func (exb *ExpressionBuilder) Compile(text string) (ex compiled.Expression, typ descriptor.ValueType, err error) {
	typ = descriptor.VALUE_TYPE_UNSPECIFIED

	expr, err := Parse(text)
	if err != nil {
		return
	}

	out := &expression{
		provider: exb.provider,
		expr:     expr,
	}
	ex = out

	checked, err := Check(expr, exb.env)
	if err != nil {
		return
	}

	typ = recoverType(checked.TypeMap[expr.Id])
	out.eval = exb.interpreter.NewInterpretable(interpreter.NewCheckedProgram(checked))
	return
}
