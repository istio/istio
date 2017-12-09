// Copyright 2017 Istio Authors
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

package compiled

import (
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/expr"
	"istio.io/istio/mixer/pkg/il/compiler"
	"istio.io/istio/mixer/pkg/il/interpreter"
	"istio.io/istio/mixer/pkg/il/runtime"
)

// ExpressionBuilder is used to create a set of pre-compiled expressions, backed by the same program and interpreter
// instance. It is meant to be used to create a large number of precompiled expressions that are backed by an efficient
// set of shared, immutable objects.
type ExpressionBuilder struct {
	compiler    *compiler.Compiler
	interpreter *interpreter.Interpreter
}

// NewBuilder returns a new ExpressionBuilder
func NewBuilder(finder expr.AttributeDescriptorFinder) *ExpressionBuilder {
	return newBuilder(finder, allFunctions, allExterns)
}

func newBuilder(finder expr.AttributeDescriptorFinder, functions map[string]expr.FunctionMetadata, externs map[string]interpreter.Extern) *ExpressionBuilder {
	c := compiler.New(finder, functions)
	return &ExpressionBuilder{
		compiler:    c,
		interpreter: interpreter.New(c.Program(), externs),
	}
}

// Compile the given text and return a pre-compiled expression object.
func (e *ExpressionBuilder) Compile(text string) (Expression, error) {
	fnID, err := e.compiler.CompileExpression(text)
	if err != nil {
		return nil, err
	}

	return expression{
		interpreter: e.interpreter,
		fnID:        fnID,
	}, nil
}

type expression struct {
	interpreter *interpreter.Interpreter
	fnID        uint32
}

var _ Expression = expression{}

func (e expression) Evaluate(attributes attribute.Bag) (interface{}, error) {
	r, err := e.interpreter.EvalFnID(e.fnID, attributes)
	if err != nil {
		return nil, err
	}

	return r.AsInterface(), nil
}

func (e expression) EvaluateBoolean(attributes attribute.Bag) (bool, error) {
	r, err := e.interpreter.EvalFnID(e.fnID, attributes)
	if err != nil {
		return false, err
	}

	return r.AsBool(), nil
}

// TODO: This should be replaced with a common, shared context, instead of a singleton global.
var allFunctions = expr.FuncMap(runtime.ExternFunctionMetadata)
var allExterns = runtime.Externs
