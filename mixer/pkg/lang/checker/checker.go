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

package checker

import (
	"fmt"

	dpb "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/lang"
	"istio.io/istio/mixer/pkg/lang/ast"
	"istio.io/pkg/attribute"
)

// TypeChecker validates a given expression for type safety.
type TypeChecker interface {
	// EvalType produces the type of an expression or an error if the type cannot be evaluated.
	// TODO: we probably want to use a golang type rather than pb.ValueType (a proto).
	EvalType(expr string) (dpb.ValueType, error)
}

// checker for a c-like expression language.
type checker struct {
	finder    attribute.AttributeDescriptorFinder
	functions map[string]ast.FunctionMetadata
}

func (c *checker) EvalType(expression string) (dpb.ValueType, error) {
	v, err := ast.Parse(expression)
	if err != nil {
		return dpb.VALUE_TYPE_UNSPECIFIED, fmt.Errorf("failed to parse expression '%s': %v", expression, err)
	}
	return v.EvalType(c.finder, c.functions)
}

// NewTypeChecker returns a new TypeChecker implementation.
func NewTypeChecker(finder attribute.AttributeDescriptorFinder) TypeChecker {
	return &checker{
		finder:    finder,
		functions: ast.FuncMap(lang.ExternFunctionMetadata),
	}
}
