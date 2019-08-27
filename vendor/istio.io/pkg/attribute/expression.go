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

package attribute

// Expression represents a precompiled expression that can be immediately evaluated.
// It holds no cache and does not listen to any events. If the configuration changes, the CompiledExpression needs
// to be discarded and created again.
type Expression interface {
	// Evaluate evaluates this compiled expression against the attribute bag.
	Evaluate(attributes Bag) (interface{}, error)

	// EvaluateBoolean evaluates this compiled expression against the attribute bag and returns the result as boolean.
	// panics if the expression does not return a boolean.
	EvaluateBoolean(attributes Bag) (bool, error)

	// EvaluateString evaluates this compiled expression against the attribute bag and returns the result as string.
	// panics if the expression does not return a string.
	EvaluateString(attributes Bag) (string, error)

	// EvaluateDouble evaluates this compiled expression against the attribute bag and returns the result as float64.
	// panics if the expression does not return a float64.
	EvaluateDouble(attribute Bag) (float64, error)

	// EvaluateInteger evaluates this compiled expression against the attribute bag and returns the result as int64.
	// panics if the expression does not return a int64.
	EvaluateInteger(attribute Bag) (int64, error)
}
