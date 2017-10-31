// Copyright 2017 Istio Authors.
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
	"errors"

	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"
)

// EvalBody is a function that will be executed when expr.Evaluator.Eval and expr.Evaluator.EvalString are called.
type EvalBody func(string, attribute.Bag) (interface{}, error)

type fakeEval struct {
	expr.PredicateEvaluator
	expr.TypeChecker

	body EvalBody
}

// NewFakeEval constructs a new Evaluator with the provided body.
func NewFakeEval(body EvalBody) expr.Evaluator {
	return &fakeEval{body: body}
}

// NewIDEval constructs a new Evaluator that return the expression string its provided.
func NewIDEval() expr.Evaluator {
	return NewFakeEval(func(e string, _ attribute.Bag) (interface{}, error) {
		return e, nil
	})
}

// NewErrEval constructs a new Evaluator that always returns an error.
func NewErrEval() expr.Evaluator {
	return NewFakeEval(func(_ string, _ attribute.Bag) (interface{}, error) {
		return nil, errors.New("eval error")
	})
}

func (f *fakeEval) Eval(expression string, attrs attribute.Bag) (interface{}, error) {
	return f.body(expression, attrs)
}

func (f *fakeEval) EvalString(expression string, attrs attribute.Bag) (string, error) {
	r, err := f.body(expression, attrs)
	if err != nil {
		return "", err
	}
	return r.(string), err
}
