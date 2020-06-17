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

package cel

import (
	"fmt"

	celgo "github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/debug"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	descriptor "istio.io/api/policy/v1beta1"
	"istio.io/pkg/attribute"
)

// LanguageMode controls parsing and evaluation properties of the expression builder
type LanguageMode int

const (
	// CEL mode uses CEL syntax and runtime
	CEL LanguageMode = iota

	// LegacySyntaxCEL uses CEXL syntax and CEL runtime
	LegacySyntaxCEL
)

// ExpressionBuilder creates a CEL interpreter from an attribute manifest.
type ExpressionBuilder struct {
	mode     LanguageMode
	provider *attributeProvider
	env      *celgo.Env
}

type expression struct {
	expr     *exprpb.Expr
	provider *attributeProvider
	program  celgo.Program
}

func (ex *expression) Evaluate(bag attribute.Bag) (out interface{}, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic during evaluation: %v", r)
		}
	}()

	result, _, err := ex.program.Eval(ex.provider.newActivation(bag))
	if err != nil {
		return nil, err
	}
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
func NewBuilder(finder attribute.AttributeDescriptorFinder, mode LanguageMode) *ExpressionBuilder {
	provider := newAttributeProvider(finder.Attributes())
	env := provider.newEnvironment()

	return &ExpressionBuilder{
		mode:     mode,
		provider: provider,
		env:      env,
	}
}

// Compile the given text and return a pre-compiled expression object.
func (exb *ExpressionBuilder) check(text string) (checked *celgo.Ast, typ descriptor.ValueType, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic during CEL parsing of expression %q", text)
		}
	}()

	typ = descriptor.VALUE_TYPE_UNSPECIFIED

	if exb.mode == LegacySyntaxCEL {
		text, err = sourceCEXLToCEL(text)
		if err != nil {
			return
		}
	}

	parsed, iss := exb.env.Parse(text)
	if iss != nil && iss.Err() != nil {
		err = iss.Err()
		return
	}

	checked, iss = exb.env.Check(parsed)
	if iss != nil && iss.Err() != nil {
		err = iss.Err()
		return
	}

	typ = recoverType(checked.ResultType())
	return
}

// Compile the given text and return a pre-compiled expression object.
func (exb *ExpressionBuilder) Compile(text string) (attribute.Expression, descriptor.ValueType, error) {
	checked, typ, err := exb.check(text)
	if err != nil {
		return nil, typ, err
	}

	program, err := exb.env.Program(checked, standardOverloads)
	if err != nil {
		return nil, typ, err
	}

	return &expression{
		provider: exb.provider,
		expr:     checked.Expr(),
		program:  program,
	}, typ, nil
}

// EvalType returns the type of an expression
func (exb *ExpressionBuilder) EvalType(text string) (descriptor.ValueType, error) {
	_, typ, err := exb.check(text)
	return typ, err
}
