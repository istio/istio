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
	"reflect"
	"testing"

	"github.com/golang/protobuf/proto"
	structpb "github.com/golang/protobuf/ptypes/struct"
)

func TestInt_Add(t *testing.T) {
	if !Int(4).Add(Int(-3)).Equal(Int(1)).(Bool) {
		t.Error("Adding two ints did not match expected value.")
	}
	if !IsError(Int(-1).Add(String("-1"))) {
		t.Error("Adding non-int to int was not an error.")
	}
}

func TestInt_Compare(t *testing.T) {
	lt := Int(-1300)
	gt := Int(204)
	if !lt.Compare(gt).Equal(IntNegOne).(Bool) {
		t.Error("Comparison did not yield - 1")
	}
	if !gt.Compare(lt).Equal(IntOne).(Bool) {
		t.Error("Comparison did not yield 1")
	}
	if !gt.Compare(gt).Equal(IntZero).(Bool) {
		t.Error(("Comparison did not yield 0"))
	}
	if !IsError(gt.Compare(TypeType)) {
		t.Error("Got comparison value, expected error.")
	}
}

func TestInt_ConvertToNative_Error(t *testing.T) {
	val, err := Int(1).ConvertToNative(jsonStructType)
	if err == nil {
		t.Errorf("Got '%v', expected error", val)
	}
}

func TestInt_ConvertToNative_Int32(t *testing.T) {
	val, err := Int(20050).ConvertToNative(reflect.TypeOf(int32(0)))
	if err != nil {
		t.Error(err)
	} else if val.(int32) != 20050 {
		t.Errorf("Got '%v', expected 20050", val)
	}
}

func TestInt_ConvertToNative_Int64(t *testing.T) {
	// Value greater than max int32.
	val, err := Int(4147483648).ConvertToNative(reflect.TypeOf(int64(0)))
	if err != nil {
		t.Error(err)
	} else if val.(int64) != 4147483648 {
		t.Errorf("Got '%v', expected 4147483648", val)
	}
}

func TestInt_ConvertToNative_Json(t *testing.T) {
	val, err := Int(4147483648).ConvertToNative(jsonValueType)
	if err != nil {
		t.Error(err)
	} else if !proto.Equal(val.(proto.Message),
		&structpb.Value{Kind: &structpb.Value_NumberValue{NumberValue: 4147483648}}) {
		t.Errorf("Got '%v', expected a json number", val)
	}
}

func TestInt_ConvertToNative_Ptr_Int32(t *testing.T) {
	ptrType := int32(0)
	val, err := Int(20050).ConvertToNative(reflect.TypeOf(&ptrType))
	if err != nil {
		t.Error(err)
	} else if *val.(*int32) != 20050 {
		t.Errorf("Got '%v', expected 20050", val)
	}
}

func TestInt_ConvertToNative_Ptr_Int64(t *testing.T) {
	// Value greater than max int32.
	ptrType := int64(0)
	val, err := Int(4147483648).ConvertToNative(reflect.TypeOf(&ptrType))
	if err != nil {
		t.Error(err)
	} else if *val.(*int64) != 4147483648 {
		t.Errorf("Got '%v', expected 4147483648", val)
	}
}

func TestInt_ConvertToType(t *testing.T) {
	if !Int(-4).ConvertToType(IntType).Equal(Int(-4)).(Bool) {
		t.Error("Unsuccessful type conversion to int")
	}
	if !Int(-4).ConvertToType(UintType).Equal(Uint(18446744073709551612)).(Bool) {
		t.Error("Unsuccessful type conversion to uint")
	}
	if !Int(-4).ConvertToType(DoubleType).Equal(Double(-4)).(Bool) {
		t.Error("Unsuccessful type conversion to double")
	}
	if !Int(-4).ConvertToType(StringType).Equal(String("-4")).(Bool) {
		t.Error("Unsuccessful type conversion to string")
	}
	if !Int(-4).ConvertToType(TypeType).Equal(IntType).(Bool) {
		t.Error("Unsuccessful type conversion to type")
	}
	if !IsError(Int(-4).ConvertToType(DurationType)) {
		t.Error("Got duration, expected error.")
	}
}

func TestInt_Divide(t *testing.T) {
	if !Int(3).Divide(Int(2)).Equal(Int(1)).(Bool) {
		t.Error("Dividing two ints did not match expectations.")
	}
	if !IsError(IntZero.Divide(IntZero)) {
		t.Error("Divide by zero did not cause error.")
	}
	if !IsError(Int(1).Divide(Double(-1))) {
		t.Error("Division permitted without express type-conversion.")
	}
}

func TestInt_Equal(t *testing.T) {
	if !IsError(Int(0).Equal(False)) {
		t.Error("Int equal to non-int type resulted in non-error.")
	}
}

func TestInt_Modulo(t *testing.T) {
	if !Int(21).Modulo(Int(2)).Equal(Int(1)).(Bool) {
		t.Error("Unexpected result from modulus operator.")
	}
	if !IsError(Int(21).Modulo(IntZero)) {
		t.Error("Modulus by zero did not cause error.")
	}
	if !IsError(Int(21).Modulo(uintZero)) {
		t.Error("Modulus permitted between different types without type conversion.")
	}
}

func TestInt_Multiply(t *testing.T) {
	if !Int(2).Multiply(Int(-2)).Equal(Int(-4)).(Bool) {
		t.Error("Multiplying two values did not match expectations.")
	}
	if !IsError(Int(1).Multiply(Double(-4.0))) {
		t.Error("Multiplication permitted without express type-conversion.")
	}
}

func TestInt_Negate(t *testing.T) {
	if !Int(1).Negate().Equal(Int(-1)).(Bool) {
		t.Error("Negating int value did not succeed")
	}
}

func TestInt_Subtract(t *testing.T) {
	if !Int(4).Subtract(Int(-3)).Equal(Int(7)).(Bool) {
		t.Error("Subtracting two ints did not match expected value.")
	}
	if !IsError(Int(1).Subtract(Uint(1))) {
		t.Error("Subtraction permitted without express type-conversion.")
	}
}
