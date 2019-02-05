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

	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// Double type that implements ref.Val, comparison, and mathematical
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
func (d Double) Add(other ref.Val) ref.Val {
	if DoubleType != other.Type() {
		return ValOrErr(other, "no such overload")
	}
	return d + other.(Double)
}

// Compare implements traits.Comparer.Compare.
func (d Double) Compare(other ref.Val) ref.Val {
	if DoubleType != other.Type() {
		return ValOrErr(other, "no such overload")
	}
	if d < other.(Double) {
		return IntNegOne
	}
	if d > other.(Double) {
		return IntOne
	}
	return IntZero
}

// ConvertToNative implements ref.Val.ConvertToNative.
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

// ConvertToType implements ref.Val.ConvertToType.
func (d Double) ConvertToType(typeVal ref.Type) ref.Val {
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
func (d Double) Divide(other ref.Val) ref.Val {
	if DoubleType != other.Type() {
		return ValOrErr(other, "no such overload")
	}
	if other.(Double) == Double(0) {
		return NewErr("divide by zero")
	}
	return d / other.(Double)
}

// Equal implements ref.Val.Equal.
func (d Double) Equal(other ref.Val) ref.Val {
	if DoubleType != other.Type() {
		return ValOrErr(other, "no such overload")
	}
	// TODO: Handle NaNs properly.
	return Bool(d == other.(Double))
}

// Multiply implements traits.Multiplier.Multiply.
func (d Double) Multiply(other ref.Val) ref.Val {
	if DoubleType != other.Type() {
		return ValOrErr(other, "no such overload")
	}
	return d * other.(Double)
}

// Negate implements traits.Negater.Negate.
func (d Double) Negate() ref.Val {
	return -d
}

// Subtract implements traits.Subtractor.Subtract.
func (d Double) Subtract(subtrahend ref.Val) ref.Val {
	if DoubleType != subtrahend.Type() {
		return ValOrErr(subtrahend, "no such overload")
	}
	return d - subtrahend.(Double)
}

// Type implements ref.Val.Type.
func (d Double) Type() ref.Type {
	return DoubleType
}

// Value implements ref.Val.Value.
func (d Double) Value() interface{} {
	return float64(d)
}
