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

// Uint type implementation which supports comparison and math operators.
type Uint uint64

var (
	// UintType singleton.
	UintType = NewTypeValue("uint",
		traits.AdderType,
		traits.ComparerType,
		traits.DividerType,
		traits.ModderType,
		traits.MultiplierType,
		traits.SubtractorType)
)

// Int constants
const (
	uintZero = Uint(0)
)

// Add implements traits.Adder.Add.
func (i Uint) Add(other ref.Value) ref.Value {
	if UintType != other.Type() {
		return ValOrErr(other, "no such overload")
	}
	return i + other.(Uint)
}

// Compare implements traits.Comparer.Compare.
func (i Uint) Compare(other ref.Value) ref.Value {
	if UintType != other.Type() {
		return ValOrErr(other, "no such overload")
	}
	if i < other.(Uint) {
		return IntNegOne
	}
	if i > other.(Uint) {
		return IntOne
	}
	return IntZero
}

// ConvertToNative implements ref.Value.ConvertToNative.
func (i Uint) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	value := i.Value()
	switch typeDesc.Kind() {
	case reflect.Uint32:
		return uint32(value.(uint64)), nil
	case reflect.Uint64:
		return value, nil
	case reflect.Ptr:
		if typeDesc == jsonValueType {
			return &structpb.Value{
				Kind: &structpb.Value_NumberValue{
					NumberValue: float64(i)}}, nil
		}
		switch typeDesc.Elem().Kind() {
		case reflect.Uint32:
			p := uint32(i)
			return &p, nil
		case reflect.Uint64:
			p := uint64(i)
			return &p, nil
		}
	case reflect.Interface:
		if reflect.TypeOf(i).Implements(typeDesc) {
			return i, nil
		}
	}
	return nil, fmt.Errorf("unsupported type conversion from 'uint' to %v", typeDesc)
}

// ConvertToType implements ref.Value.ConvertToType.
func (i Uint) ConvertToType(typeVal ref.Type) ref.Value {
	switch typeVal {
	case IntType:
		return Int(i)
	case UintType:
		return i
	case DoubleType:
		return Double(i)
	case StringType:
		return String(fmt.Sprintf("%d", uint64(i)))
	case TypeType:
		return UintType
	}
	return NewErr("type conversion error from '%s' to '%s'", UintType, typeVal)
}

// Divide implements traits.Divider.Divide.
func (i Uint) Divide(other ref.Value) ref.Value {
	if UintType != other.Type() {
		return ValOrErr(other, "no such overload")
	}
	otherUint := other.(Uint)
	if otherUint == uintZero {
		return NewErr("divide by zero")
	}
	return i / otherUint
}

// Equal implements ref.Value.Equal.
func (i Uint) Equal(other ref.Value) ref.Value {
	if UintType != other.Type() {
		return ValOrErr(other, "no such overload")
	}
	return Bool(i == other.(Uint))
}

// Modulo implements traits.Modder.Modulo.
func (i Uint) Modulo(other ref.Value) ref.Value {
	if UintType != other.Type() {
		return ValOrErr(other, "no such overload")
	}
	otherUint := other.(Uint)
	if otherUint == uintZero {
		return NewErr("modulus by zero")
	}
	return i % otherUint
}

// Multiply implements traits.Multiplier.Multiply.
func (i Uint) Multiply(other ref.Value) ref.Value {
	if UintType != other.Type() {
		return ValOrErr(other, "no such overload")
	}
	return i * other.(Uint)
}

// Subtract implements traits.Subtractor.Subtract.
func (i Uint) Subtract(subtrahend ref.Value) ref.Value {
	if UintType != subtrahend.Type() {
		return ValOrErr(subtrahend, "no such overload")
	}
	return i - subtrahend.(Uint)
}

// Type implements ref.Value.Type.
func (i Uint) Type() ref.Type {
	return UintType
}

// Value implements ref.Value.Value.
func (i Uint) Value() interface{} {
	return uint64(i)
}
