// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package types

import (
	"fmt"
	"reflect"
	"strconv"

	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// Bool type that implements ref.Val and supports comparison and negation.
type Bool bool

var (
	// BoolType singleton.
	BoolType = NewTypeValue("bool",
		traits.ComparerType,
		traits.NegatorType)
)

// Boolean constants
var (
	False = Bool(false)
	True  = Bool(true)
)

// Compare implements traits.Comparer.Compare.
func (b Bool) Compare(other ref.Val) ref.Val {
	if BoolType != other.Type() {
		return ValOrErr(other, "no such overload")
	}
	otherBool := other.(Bool)
	if b == otherBool {
		return IntZero
	}
	if !b && otherBool {
		return IntNegOne
	}
	return IntOne
}

// ConvertToNative implements ref.Val.ConvertToNative.
func (b Bool) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	switch typeDesc.Kind() {
	case reflect.Bool:
		return bool(b), nil
	case reflect.Ptr:
		if typeDesc == jsonValueType {
			return &structpb.Value{
				Kind: &structpb.Value_BoolValue{
					BoolValue: b.Value().(bool)}}, nil
		}
		if typeDesc.Elem().Kind() == reflect.Bool {
			p := bool(b)
			return &p, nil
		}
	case reflect.Interface:
		if reflect.TypeOf(b).Implements(typeDesc) {
			return b, nil
		}
	}
	return nil, fmt.Errorf("type conversion error from bool to '%v'", typeDesc)
}

// ConvertToType implements ref.Val.ConvertToType.
func (b Bool) ConvertToType(typeVal ref.Type) ref.Val {
	switch typeVal {
	case StringType:
		return String(strconv.FormatBool(bool(b)))
	case BoolType:
		return b
	case TypeType:
		return BoolType
	}
	return NewErr("type conversion error from '%v' to '%v'", BoolType, typeVal)
}

// Equal implements ref.Val.Equal.
func (b Bool) Equal(other ref.Val) ref.Val {
	if BoolType != other.Type() {
		return ValOrErr(other, "no such overload")
	}
	return Bool(b == other.(Bool))
}

// Negate implements traits.Negater.Negate.
func (b Bool) Negate() ref.Val {
	return !b
}

// Type implements ref.Val.Type.
func (b Bool) Type() ref.Type {
	return BoolType
}

// Value implements ref.Val.Value.
func (b Bool) Value() interface{} {
	return bool(b)
}

// IsBool returns whether the input ref.Val or ref.Type is equal to BoolType.
func IsBool(elem interface{}) bool {
	switch elem.(type) {
	case ref.Type:
		return elem == BoolType
	case ref.Val:
		return IsBool(elem.(ref.Val).Type())
	}
	return false
}
