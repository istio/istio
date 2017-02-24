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

package expr

import (
	"reflect"
	"strings"

	config "istio.io/api/mixer/v1/config/descriptor"
)

// Func defines the interface that every expression function provider must implement.
type Func interface {
	// Name uniquely identified the function.
	Name() string

	// ReturnType specifies the return type of this function.
	ReturnType() config.ValueType

	// ArgTypes specifies the argument types in order expected by the function.
	ArgTypes() []config.ValueType

	// NullArgs specifies if the function accepts null args
	AcceptsNulls() bool

	// Call performs the function call. It is guaranteed
	// that call will be made with correct types and arity.
	// may panic.
	Call([]interface{}) interface{}
}

type baseFunc struct {
	name         string
	argTypes     []config.ValueType
	retType      config.ValueType
	acceptsNulls bool
}

func (f *baseFunc) Name() string                 { return f.name }
func (f *baseFunc) ReturnType() config.ValueType { return f.retType }
func (f *baseFunc) ArgTypes() []config.ValueType { return f.argTypes }
func (f *baseFunc) AcceptsNulls() bool           { return f.acceptsNulls }

type eqFunc struct {
	*baseFunc
	invert bool
}

// newEQ returns dynamically typed equality fn.
// treats strings specially with prefix and suffix '*' matches
func newEQ() Func {
	return &eqFunc{
		baseFunc: &baseFunc{
			name:     "EQ",
			retType:  config.BOOL,
			argTypes: []config.ValueType{config.VALUE_TYPE_UNSPECIFIED, config.VALUE_TYPE_UNSPECIFIED},
		},
	}
}

// newNEQ returns inverse of newEQFunc fn.
func newNEQ() Func {
	return &eqFunc{
		baseFunc: &baseFunc{
			name:     "NEQ",
			retType:  config.BOOL,
			argTypes: []config.ValueType{config.VALUE_TYPE_UNSPECIFIED, config.VALUE_TYPE_UNSPECIFIED},
		},
		invert: true,
	}
}

func (f *eqFunc) Call(args []interface{}) interface{} {
	res := f.call(args)
	if f.invert {
		return !res
	}
	return res
}

func (f *eqFunc) call(args []interface{}) bool {
	var s0 string
	var s1 string
	var ok bool

	if s0, ok = args[0].(string); !ok {
		return reflect.DeepEqual(args[0], args[1])
	}
	if s1, ok = args[1].(string); !ok {
		// s0 is string and s1 is not
		return false
	}
	return matchWithWildcards(s0, s1)
}

// matchWithWildcards     s0 == ns1.*   --> ns1 should be a prefix of s0
// s0 == *.ns1 --> ns1 should be a suffix of s0
// TODO allow escaping *
func matchWithWildcards(s0 string, s1 string) bool {
	if strings.HasSuffix(s1, "*") {
		return strings.HasPrefix(s0, s1[:len(s1)-1])
	}
	if strings.HasPrefix(s1, "*") {
		return strings.HasSuffix(s0, s1[1:])
	}
	return s0 == s1
}

type lAndFunc struct {
	*baseFunc
}

// newLAND returns a binary logical AND fn.
func newLAND() Func {
	return &lAndFunc{
		&baseFunc{
			name:     "LAND",
			retType:  config.BOOL,
			argTypes: []config.ValueType{config.BOOL, config.BOOL},
		},
	}
}

// Call returns true if both elements true
// may panic
func (f *lAndFunc) Call(args []interface{}) interface{} {
	return args[0].(bool) && args[1].(bool)
}

type lOrFunc struct {
	*baseFunc
}

// newLOR returns logical OR fn.
func newLOR() Func {
	return &lOrFunc{
		&baseFunc{
			name:     "LOR",
			retType:  config.BOOL,
			argTypes: []config.ValueType{config.BOOL, config.BOOL},
		},
	}
}

// Call should return true if at least one element is true
func (f *lOrFunc) Call(args []interface{}) interface{} {
	return args[0].(bool) || args[1].(bool)
}

// applies to non bools.
type orFunc struct {
	*baseFunc
}

// newOR returns an OR fn that selects first non empty argument.
// types can be anything, but all args must be of the same type.
func newOR() Func {
	return &orFunc{
		baseFunc: &baseFunc{
			name:         "OR",
			retType:      config.VALUE_TYPE_UNSPECIFIED,
			argTypes:     []config.ValueType{config.VALUE_TYPE_UNSPECIFIED, config.VALUE_TYPE_UNSPECIFIED},
			acceptsNulls: true,
		},
	}
}

// Call selects first non empty argument
// may return nil
func (f *orFunc) Call(args []interface{}) interface{} {
	if args[0] == nil {
		return args[1]
	}
	v := reflect.ValueOf(args[0])
	if v.Interface() != reflect.Zero(v.Type()).Interface() {
		return args[0]
	}
	return args[1]
}

// func (Value) MapIndex
type indexFunc struct {
	*baseFunc
}

// newIndex returns a map accessor fn.
func newIndex() Func {
	return &indexFunc{
		baseFunc: &baseFunc{
			name:     "INDEX",
			retType:  config.VALUE_TYPE_UNSPECIFIED,
			argTypes: []config.ValueType{config.STRING_MAP, config.STRING},
		},
	}
}

// Call returns map[key]
// panics if arg[0] is not a map
func (f *indexFunc) Call(args []interface{}) interface{} {
	m := reflect.ValueOf(args[0])
	k := reflect.ValueOf(args[1])
	if v := m.MapIndex(k); v.IsValid() {
		return v.Interface()
	}

	return nil
}

func inventory() []Func {
	return []Func{
		newEQ(),
		newNEQ(),
		newOR(),
		newLOR(),
		newLAND(),
		newIndex(),
	}
}

// FuncMap provides inventory of available functions.
func FuncMap() map[string]Func {
	m := make(map[string]Func)
	for _, fn := range inventory() {
		m[fn.Name()] = fn
	}
	return m
}
