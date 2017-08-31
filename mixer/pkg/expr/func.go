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
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"

	multierror "github.com/hashicorp/go-multierror"

	config "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/attribute"
)

// FuncBase defines the interface that every expression function must implement.
type FuncBase interface {
	// Name uniquely identifies the function.
	Name() string

	// ReturnType specifies the return type of this function.
	ReturnType() config.ValueType

	// ArgTypes specifies the argument types in order expected by the function.
	ArgTypes() []config.ValueType
}

// Func implements a function call.
// It needs to know details about Expressions and attribute bag.
type Func interface {
	FuncBase

	// Call performs the function call. It is guaranteed
	// that call will be made with correct arity.
	// may panic.
	Call(attrs attribute.Bag, args []*Expression, fMap map[string]FuncBase) (interface{}, error)
}

// baseFunc is basetype for many funcs
type baseFunc struct {
	name         string
	argTypes     []config.ValueType
	retType      config.ValueType
	acceptsNulls bool
}

func (f *baseFunc) Name() string                 { return f.name }
func (f *baseFunc) ReturnType() config.ValueType { return f.retType }
func (f *baseFunc) ArgTypes() []config.ValueType { return f.argTypes }

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

func (f *eqFunc) Call(attrs attribute.Bag, args []*Expression, fMap map[string]FuncBase) (interface{}, error) {
	arg0, err := args[0].Eval(attrs, fMap)
	if err != nil {
		return nil, err
	}

	arg1, err := args[1].Eval(attrs, fMap)
	if err != nil {
		return nil, err
	}

	res := f.call(arg0, arg1)

	if f.invert {
		return !res, nil
	}
	return res, nil
}

func (f *eqFunc) call(args0 interface{}, args1 interface{}) bool {
	switch s0 := args0.(type) {
	default:
		return reflect.DeepEqual(args0, args1)
	case bool, int64, float64:
		return args0 == args1
	case string:
		var s1 string
		var ok bool
		if s1, ok = args1.(string); !ok {
			// s0 is string and s1 is not
			return false
		}
		return matchWithWildcards(s0, s1)
	}
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
func (f *lAndFunc) Call(attrs attribute.Bag, args []*Expression, fMap map[string]FuncBase) (interface{}, error) {
	return logicalAndOr(false, attrs, args, fMap)
}

func logicalAndOr(exitVal bool, attrs attribute.Bag, args []*Expression, fMap map[string]FuncBase) (interface{}, error) {
	for _, arg := range args {
		ret, err := arg.Eval(attrs, fMap)
		if err != nil {
			return nil, err
		}
		if ret == exitVal {
			return exitVal, nil
		}
	}

	return !exitVal, nil
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
func (f *lOrFunc) Call(attrs attribute.Bag, args []*Expression, fMap map[string]FuncBase) (interface{}, error) {
	return logicalAndOr(true, attrs, args, fMap)
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

// Call selects first non empty argument / non erroneous
// may return nil
func (f *orFunc) Call(attrs attribute.Bag, args []*Expression, fMap map[string]FuncBase) (interface{}, error) {
	var me *multierror.Error
	for i, arg := range args {
		ret, err := arg.Eval(attrs, fMap)

		if err != nil {
			me = multierror.Append(me, err)
			if i == len(args)-1 {
				return nil, fmt.Errorf("error(s) evaluating OR: %v", me.ErrorOrNil())
			}
			continue
		}

		// treating empty strings as nil, since
		// go strings cannot be nil
		if ret != nil && ret != "" {
			return ret, nil
		}
	}

	return nil, nil
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
			retType:  config.STRING,
			argTypes: []config.ValueType{config.STRING_MAP, config.STRING},
		},
	}
}

func (f *indexFunc) Call(attrs attribute.Bag, args []*Expression, fMap map[string]FuncBase) (interface{}, error) {
	mp, err := args[0].Var.Eval(attrs)
	if err != nil {
		return nil, err
	}
	key, err := args[1].Eval(attrs, fMap)
	if err != nil {
		return nil, err
	}
	return mp.(map[string]string)[key.(string)], nil
}

// func (string) []uint8
type ipFunc struct {
	*baseFunc
}

// newIP returns a fn that converts strings to IP_ADDRESSes.
func newIP() Func {
	return &ipFunc{
		baseFunc: &baseFunc{
			name:     "ip",
			retType:  config.IP_ADDRESS,
			argTypes: []config.ValueType{config.STRING},
		},
	}
}

// TODO: fix when proper net.IP support is added to Mixer.
func (f *ipFunc) Call(attrs attribute.Bag, args []*Expression, fMap map[string]FuncBase) (interface{}, error) {
	val, err := args[0].Eval(attrs, fMap)
	if err != nil {
		return nil, err
	}
	rawIP, ok := val.(string)
	if !ok {
		return nil, errors.New("input to 'ip' func was not a string")
	}
	if ip := net.ParseIP(rawIP); ip != nil {
		return []uint8(ip), nil
	}
	return nil, fmt.Errorf("could not convert '%s' to IP_ADDRESS", rawIP)
}

func inventory() []FuncBase {
	return []FuncBase{
		newEQ(),
		newNEQ(),
		newOR(),
		newLOR(),
		newLAND(),
		newIndex(),
		newIP(),
	}
}

// FuncMap provides inventory of available functions.
func FuncMap() map[string]FuncBase {
	m := make(map[string]FuncBase)
	for _, fn := range inventory() {
		m[fn.Name()] = fn
	}
	return m
}
