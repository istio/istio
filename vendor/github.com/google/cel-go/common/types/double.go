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

	"github.com/golang/protobuf/ptypes/struct"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// Double type that implements ref.Value, comparison, and mathematical
// operations.
type Double float64

var (
	// DoubleType singleton.
	DoubleType = NewTypeValue("double",
		traits.AdderType,
		traits.ComparerType,
		traits.DividerType,
		traits.MultiplierType,
		traits.NegatorType,
		traits.SubtractorType)
)

// Add implements traits.Adder.Add.
func (d Double) Add(other ref.Value) ref.Value {
	if DoubleType != other.Type() {
		return NewErr("unsupported overload")
	}
	return d + other.(Double)
}

// Compare implements traits.Comparer.Compare.
func (d Double) Compare(other ref.Value) ref.Value {
	if DoubleType != other.Type() {
		return NewErr("unsupported overload")
	}
	if d < other.(Double) {
		return IntNegOne
	}
	if d > other.(Double) {
		return IntOne
	}
	return IntZero
}

// ConvertToNative implements ref.Value.ConvertToNative.
func (d Double) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	switch typeDesc.Kind() {
	case reflect.Float32:
		return float32(d), nil
	case reflect.Float64:
		return float64(d), nil
	case reflect.Ptr:
		if typeDesc == jsonValueType {
			return &structpb.Value{
				Kind: &structpb.Value_NumberValue{
					NumberValue: float64(d)}}, nil
		}
		switch typeDesc.Elem().Kind() {
		case reflect.Float32:
			p := float32(d)
			return &p, nil
		case reflect.Float64:
			p := float64(d)
			return &p, nil
		}
	case reflect.Interface:
		if reflect.TypeOf(d).Implements(typeDesc) {
			return d, nil
		}
	}
	return nil, fmt.Errorf("type conversion error from Double to '%v'", typeDesc)
}

// ConvertToType implements ref.Value.ConvertToType.
func (d Double) ConvertToType(typeVal ref.Type) ref.Value {
	switch typeVal {
	case IntType:
		return Int(float64(d))
	case UintType:
		return Uint(float64(d))
	case DoubleType:
		return d
	case StringType:
		return String(fmt.Sprintf("%g", float64(d)))
	case TypeType:
		return DoubleType
	}
	return NewErr("type conversion error from '%s' to '%s'", DoubleType, typeVal)
}

// Divide implements traits.Divider.Divide.
func (d Double) Divide(other ref.Value) ref.Value {
	if DoubleType != other.Type() {
		return NewErr("unsupported overload")
	}
	if other.(Double) == Double(0) {
		return NewErr("divide by zero")
	}
	return d / other.(Double)
}

// Equal implements ref.Value.Equal.
func (d Double) Equal(other ref.Value) ref.Value {
	return Bool(DoubleType == other.Type() && d == other)
}

// Multiply implements traits.Multiplier.Multiply.
func (d Double) Multiply(other ref.Value) ref.Value {
	if DoubleType != other.Type() {
		return NewErr("unsupported overload")
	}
	return d * other.(Double)
}

// Negate implements traits.Negater.Negate.
func (d Double) Negate() ref.Value {
	return -d
}

// Subtract implements traits.Subtractor.Subtract.
func (d Double) Subtract(subtrahend ref.Value) ref.Value {
	if DoubleType != subtrahend.Type() {
		return NewErr("unsupported overload")
	}
	return d - subtrahend.(Double)
}

// Type implements ref.Value.Type.
func (d Double) Type() ref.Type {
	return DoubleType
}

// Value implements ref.Value.Value.
func (d Double) Value() interface{} {
	return float64(d)
}
