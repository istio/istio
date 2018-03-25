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

package checker

import (
	"fmt"

	dpb "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/lang"
	"istio.io/istio/mixer/pkg/lang/ast"
)

// TypeChecker validates a given expression for type safety.
type TypeChecker interface {
	// EvalType produces the type of an expression or an error if the type cannot be evaluated.
	// TODO: we probably want to use a golang type rather than pb.ValueType (a proto).
	EvalType(expr string, finder ast.AttributeDescriptorFinder) (dpb.ValueType, error)

	// AssertType evaluates the type of expr using the attribute set; if the evaluated type is equal to
	// the expected type we return nil, and return an error otherwise.
	AssertType(expr string, finder ast.AttributeDescriptorFinder, expectedType dpb.ValueType) error
}

// checker for a c-like expression language.
type checker struct {
	functions map[string]ast.FunctionMetadata
}

func (c *checker) EvalType(expression string, attrFinder ast.AttributeDescriptorFinder) (dpb.ValueType, error) {
	v, err := ast.Parse(expression)
	if err != nil {
		return dpb.VALUE_TYPE_UNSPECIFIED, fmt.Errorf("failed to parse expression '%s': %v", expression, err)
	}
	return v.EvalType(attrFinder, c.functions)
}

func (c *checker) AssertType(expression string, finder ast.AttributeDescriptorFinder, expectedType dpb.ValueType) error {
	if t, err := c.EvalType(expression, finder); err != nil {
		return err
	} else if t != expectedType {
		return fmt.Errorf("expression '%s' evaluated to type %v, expected type %v", expression, t, expectedType)
	}
	return nil
}

// NewTypeChecker returns a new TypeChecker implementation.
func NewTypeChecker() TypeChecker {
	return &checker{
		functions: ast.FuncMap(lang.ExternFunctionMetadata),
	}
}
