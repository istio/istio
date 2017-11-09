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

package interpreter

import (
	"fmt"
	"math"
	"time"

	"github.com/golang/glog"

	"istio.io/istio/mixer/pkg/il"
)

// Result contains the result of an evaluation performed by the interpreter.
type Result struct {
	vs string
	vi interface{}
	t  il.Type
	v1 uint32
	v2 uint32
}

// Type returns the underlying type of the value in the Result.
func (r Result) Type() il.Type {
	return r.t
}

// AsBool returns the value contained in the result as a bool. If the underlying result is not bool,
// it panics.
func (r Result) AsBool() bool {
	if r.t != il.Bool {
		panic("interpreter.Result: result is not bool")
	}
	if r.v1 == 0 {
		return false
	}
	return true
}

// AsString returns the value contained in the result as a string. Unlike other methods, it does not
// panic if the underlying value is not string and returns the string version of the data.
func (r Result) AsString() string {
	if r.t != il.String {
		glog.Infof("result.AsString converting to string from type: '%v'", r.t)
		return fmt.Sprintf("%v", r.AsInterface())
	}

	return r.vs
}

// AsInteger returns the value contained in the result as an integer. If the underlying result is not
// integer, it panics.
func (r Result) AsInteger() int64 {
	if r.t != il.Integer {
		panic("interpreter.Result: result is not integer")
	}

	return int64(r.v1) + int64(r.v2)<<32
}

// AsDouble returns the value contained in the result as a double. If the underlying result is not
// double, it panics.
func (r Result) AsDouble() float64 {
	if r.t != il.Double {
		panic("interpreter.Result: result is not double")
	}

	var t = uint64(r.v1) + uint64(r.v2)<<32

	return math.Float64frombits(t)
}

// AsDuration returns the value contained in the result as time.Duration. If the underlying result is
// not a duration, it panics.
func (r Result) AsDuration() time.Duration {
	if r.t != il.Duration {
		panic("interpreter.Result: result is not Duration")
	}

	return time.Duration(int64(r.v1) + int64(r.v2)<<32)
}

// AsInterface returns the value contained in the result as an interface{}.
func (r Result) AsInterface() interface{} {
	switch r.t {
	case il.Bool:
		return r.AsBool()
	case il.String:
		return r.AsString()
	case il.Integer:
		return r.AsInteger()
	case il.Double:
		return r.AsDouble()
	case il.Duration:
		return r.AsDuration()
	case il.Void:
		return nil
	case il.Interface:
		return r.vi
	default:
		glog.Warningf("interpreter.Result: Unknown type encountered. Returning nil. type: '%v'", r.t)
		return nil
	}
}
