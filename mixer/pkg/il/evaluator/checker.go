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

package evaluator

import (
	"fmt"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/istio/mixer/pkg/expr"
	"istio.io/istio/mixer/pkg/il/runtime"
)

// Evaluator for a c-like expression language.
type checker struct {
	functions map[string]expr.FunctionMetadata
}

func (c *checker) EvalType(expression string, attrFinder expr.AttributeDescriptorFinder) (dpb.ValueType, error) {
	v, err := expr.Parse(expression)
	if err != nil {
		return dpb.VALUE_TYPE_UNSPECIFIED, fmt.Errorf("failed to parse expression '%s': %v", expression, err)
	}
	return v.EvalType(attrFinder, c.functions)
}

func (c *checker) AssertType(expression string, finder expr.AttributeDescriptorFinder, expectedType dpb.ValueType) error {
	if t, err := c.EvalType(expression, finder); err != nil {
		return err
	} else if t != expectedType {
		return fmt.Errorf("expression '%s' evaluated to type %v, expected type %v", expression, t, expectedType)
	}
	return nil
}

// NewTypeChecker returns a new TypeChecker implementation.
func NewTypeChecker() expr.TypeChecker {
	return &checker{
		functions: expr.FuncMap(runtime.ExternFunctionMetadata),
	}
}
