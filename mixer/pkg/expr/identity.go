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
	"strings"

	"github.com/golang/glog"
	"istio.io/mixer/pkg/attribute"
)

// identity is an evaluator that expects mapExpression to be a key in the attribute bag.
// It does equality-only evaluation in EvalPredicate.
// Otherwise it does no evaluation, only lookup.
type identity struct{}

// NewIdentityEvaluator returns an evaluator that performs equality-only evaluation in EvalPredicate;
// Otherwise it uses the provided mapExpression as the key into the attribute bag.
func NewIdentityEvaluator() Evaluator {
	return identity{}
}

// Eval attempts to extract the key `mapExpression` from the attribute bag. It performs no evaluation.
func (identity) Eval(mapExpression string, bag attribute.Bag) (interface{}, error) {
	if val, found := attribute.Value(bag, mapExpression); found {
		return val, nil
	}
	return nil, fmt.Errorf("missing attribute %s", mapExpression)
}

// EvalString attempts to extract the key `mapExpression` from the set of string attributes in the bag. It performs no evaluation.
func (identity) EvalString(mapExpression string, bag attribute.Bag) (string, error) {
	if val, exists := bag.String(mapExpression); exists {
		return val, nil
	}
	return "", fmt.Errorf("missing attribute %s", mapExpression)
}

// resolve if a symbol starts with '$' attribute is accessed, otherwise it is treated as a constant
func resolve(sym string, bag attribute.Bag) (interface{}, error) {
	if !strings.HasPrefix(sym, "$") {
		return sym, nil
	}
	if len(sym) == 1 {
		return sym, fmt.Errorf("empty attribute name")
	}

	var found bool
	var ret interface{}

	if ret, found = attribute.Value(bag, sym[1:]); !found {
		return false, fmt.Errorf("unresolved attribute %s", sym[1:])
	}
	return ret, nil
}

// EvalPredicate attempts to extract the key `mapExpression` from the set of boolean attributes in the bag. It performs no evaluation.
func (identity) EvalPredicate(mapExpression string, bag attribute.Bag) (bool, error) {
	vals := strings.Split(mapExpression, "==")
	// a === b
	if len(vals) != 2 {
		return false, fmt.Errorf("invalid expression %s", mapExpression)
	}

	lsym := strings.TrimSpace(vals[0])
	rsym := strings.TrimSpace(vals[1])

	var lval interface{}
	var rval interface{}
	var err error

	if lval, err = resolve(lsym, bag); err != nil {
		return false, fmt.Errorf("error evaluating lval %s: %s", mapExpression, err.Error())
	}

	if rval, err = resolve(rsym, bag); err != nil {
		return false, fmt.Errorf("error evaluating rval %s: %s", mapExpression, err.Error())
	}

	// check if lval and rval match
	if lval == rval {
		return true, nil
	}

	glog.V(2).Infof("Predicate did not match %#v != %#v", lval, rval)
	return false, nil
}

// Validate -- everything is valid
func (identity) Validate(expr string) error { return nil }
