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
	descriptor "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/lang/ast"
	"istio.io/istio/mixer/pkg/lang/compiled"
)

// ExpressionBuilder is used to create a set of pre-compiled expressions, backed by the same program and interpreter
// instance. It is meant to be used to create a large number of precompiled expressions that are backed by an efficient
// set of shared, immutable objects.
type ExpressionBuilder struct {
}

// NewBuilder returns a new ExpressionBuilder
func NewBuilder(finder ast.AttributeDescriptorFinder) *ExpressionBuilder {
	return &ExpressionBuilder{}
}

// Compile the given text and return a pre-compiled expression object.
func (exb *ExpressionBuilder) Compile(text string) (compiled.Expression, descriptor.ValueType, error) {
	return nil, descriptor.VALUE_TYPE_UNSPECIFIED, nil
}
