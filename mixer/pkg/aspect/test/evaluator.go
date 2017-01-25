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

package test

import (
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"
)

// Evaluator is a test version of the expr.Evaluator.
type Evaluator struct {
	expr.Evaluator
}

// Eval evaluates given expression using the attribute bag.
func (t Evaluator) Eval(e string, bag attribute.Bag) (interface{}, error) {
	return e, nil
}
