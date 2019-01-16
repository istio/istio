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

	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"

	structpb "github.com/golang/protobuf/ptypes/struct"
)

// Int type that implements ref.Value as well as comparison and math operators.
type Int int64

// Int constants used for comparison results.
const (
	IntZero   = Int(0)
	IntOne    = Int(1)
	IntNegOne = Int(-1)
)

var (
	// IntType singleton.
	IntType = NewTypeValue("int",
		traits.AdderType,
		traits.ComparerType,
		traits.DividerType,
		traits.ModderType,
		traits.MultiplierType,
		traits.NegatorType,
		traits.SubtractorType)
)

// Add implements traits.Adder.Add.
func (i Int) Add(other ref.Value) ref.Value {
	if IntType != other.Type() {
		return NewErr("unsupported overload")
	}
	return i + other.(Int)
}

// Compare implements traits.Comparer.Compare.
func (i Int) Compare(other ref.Value) ref.Value {
	if IntType != other.Type() {
		return NewErr("unsupported overload")
	}
	if i < other.(Int) {
		return IntNegOne
	}
	if i > other.(Int) {
		return IntOne
	}
	return IntZero
}

// ConvertToNative implements ref.Value.ConvertToNative.
func (i Int) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	switch typeDesc.Kind() {
	case reflect.Int32:
		return reflect.ValueOf(i).Convert(typeDesc).Interface(), nil
	case reflect.Int64:
		return int64(i), nil
	case reflect.Ptr:
		if typeDesc == jsonValueType {
			return &structpb.Value{
				Kind: &structpb.Value_NumberValue{
					NumberValue: float64(i)}}, nil
		}
		switch typeDesc.Elem().Kind() {
		case reflect.Int32:
			p := int32(i)
			return &p, nil
		case reflect.Int64:
			p := int64(i)
			return &p, nil
		}
	case reflect.Interface:
		if reflect.TypeOf(i).Implements(typeDesc) {
			return i, nil
		}
	}
	return nil, fmt.Errorf("unsupported type conversion from 'int' to %v", typeDesc)
}

// ConvertToType implements ref.Value.ConvertToType.
func (i Int) ConvertToType(typeVal ref.Type) ref.Value {
	switch typeVal {
	case IntType:
		return i
	case UintType:
		return Uint(i)
	case DoubleType:
		return Double(i)
	case StringType:
		return String(fmt.Sprintf("%d", int64(i)))
	case TypeType:
		return IntType
	}
	return NewErr("type conversion error from '%s' to '%s'", IntType, typeVal)
}

// Divide implements traits.Divider.Divide.
func (i Int) Divide(other ref.Value) ref.Value {
	if IntType != other.Type() {
		return NewErr("unsupported overload")
	}
	otherInt := other.(Int)
	if otherInt == IntZero {
		return NewErr("divide by zero")
	}
	return i / otherInt
}

// Equal implements ref.Value.Equal.
func (i Int) Equal(other ref.Value) ref.Value {
	return Bool(IntType == other.Type() && i == other.(Int))
}

// Modulo implements traits.Modder.Modulo.
func (i Int) Modulo(other ref.Value) ref.Value {
	if IntType != other.Type() {
		return NewErr("unsupported overload")
	}
	otherInt := other.(Int)
	if otherInt == IntZero {
		return NewErr("modulus by zero")
	}
	return i % otherInt
}

// Multiply implements traits.Multiplier.Multiply.
func (i Int) Multiply(other ref.Value) ref.Value {
	if IntType != other.Type() {
		return NewErr("unsupported overload")
	}
	return i * other.(Int)
}

// Negate implements traits.Negater.Negate.
func (i Int) Negate() ref.Value {
	return -i
}

// Subtract implements traits.Subtractor.Subtract.
func (i Int) Subtract(subtrahend ref.Value) ref.Value {
	if IntType != subtrahend.Type() {
		return NewErr("unsupported overload")
	}
	return i - subtrahend.(Int)
}

// Type implements ref.Value.Type.
func (i Int) Type() ref.Type {
	return IntType
}

// Value implements ref.Value.Value.
func (i Int) Value() interface{} {
	return int64(i)
}
