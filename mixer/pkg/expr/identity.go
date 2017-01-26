// Copyright 2017 Google Inc.
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

package expr

import (
	"fmt"

	"istio.io/mixer/pkg/attribute"
)

// identity is an evaluator that expects mapExpression to be a key in the attribute bag. It does no evaluation, only lookup.
type identity struct{}

// NewIdentityEvaluator returns an evaluator that performs no evaluations; instead it uses the provided mapExpression
// as the key into the attribute bag.
func NewIdentityEvaluator() Evaluator {
	return identity{}
}

// Eval attempts to extract the key `mapExpression` from the attribute bag. It performs no evaluation.
func (identity) Eval(mapExpression string, bag attribute.Bag) (interface{}, error) {
	if val, found := attribute.Value(bag, mapExpression); found {
		return val, nil
	}
	return nil, fmt.Errorf("%s not in attribute bag", mapExpression)
}

// EvalString attempts to extract the key `mapExpression` from the set of string attributes in the bag. It performs no evaluation.
func (identity) EvalString(mapExpression string, bag attribute.Bag) (string, error) {
	if val, exists := bag.String(mapExpression); exists {
		return val, nil
	}
	return "", fmt.Errorf("%s not in attribute bag", mapExpression)
}

// EvalPredicate attempts to extract the key `mapExpression` from the set of boolean attributes in the bag. It performs no evaluation.
func (identity) EvalPredicate(mapExpression string, bag attribute.Bag) (bool, error) {
	if val, exists := bag.Bool(mapExpression); exists {
		return val, nil
	}
	return false, fmt.Errorf("%s not in attribute bag", mapExpression)
}

// Validate -- everything is valid
func (identity) Validate(expr string) error { return nil }
