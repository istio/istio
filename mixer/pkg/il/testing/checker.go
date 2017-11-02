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

package ilt

import (
	"fmt"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/istio/mixer/pkg/expr"
)

type testChecker struct {
	functions map[string]expr.FunctionMetadata
}

var _ expr.TypeChecker = &testChecker{}

// NewTestTypeChecker returns an instance of expr.TypeChecker that is suitable for use in tests.
// This is mainly used by pkg/config tests, which causes dependency issues if they use the TypeChecker that is
// implemented by the il/evaluator/TypeChecker.
func NewTestTypeChecker() expr.TypeChecker {
	return &testChecker{
		functions: expr.FuncMap([]expr.FunctionMetadata{}),
	}
}

func (e *testChecker) EvalType(str string, attrFinder expr.AttributeDescriptorFinder) (dpb.ValueType, error) {
	v, err := expr.Parse(str)
	if err != nil {
		return dpb.VALUE_TYPE_UNSPECIFIED, fmt.Errorf("failed to parse expression '%s': %v", str, err)
	}
	return v.EvalType(attrFinder, e.functions)
}

func (e *testChecker) AssertType(expr string, finder expr.AttributeDescriptorFinder, expectedType dpb.ValueType) error {
	if t, err := e.EvalType(expr, finder); err != nil {
		return err
	} else if t != expectedType {
		return fmt.Errorf("expression '%s' evaluated to type %v, expected type %v", expr, t, expectedType)
	}
	return nil
}
