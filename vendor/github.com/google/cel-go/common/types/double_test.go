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

func TestDouble_Add(t *testing.T) {
	if !Double(4).Add(Double(-3.5)).Equal(Double(0.5)).(Bool) {
		t.Error("Adding two doubles did not match expected value.")
	}
	if !IsError(Double(-1).Add(String("-1"))) {
		t.Error("Adding non-double to double was not an error.")
	}
}

func TestDouble_Compare(t *testing.T) {
	lt := Double(-1300)
	gt := Double(204)
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
		t.Error("Types not comparable")
	}
}

func TestDouble_ConvertToNative_Error(t *testing.T) {
	val, err := Double(-10000).ConvertToNative(reflect.TypeOf(""))
	if err == nil {
		t.Errorf("Got '%v', expected error", val)
	}
}

func TestDouble_ConvertToNative_Float32(t *testing.T) {
	val, err := Double(3.1415).ConvertToNative(reflect.TypeOf(float32(0)))
	if err != nil {
		t.Error(err)
	} else if val.(float32) != 3.1415 {
		t.Errorf("Got '%v', wanted 3.1415", val)
	}
}

func TestDouble_ConvertToNative_Float64(t *testing.T) {
	val, err := Double(30000000.1).ConvertToNative(reflect.TypeOf(float64(0)))
	if err != nil {
		t.Error(err)
	} else if val.(float64) != 30000000.1 {
		t.Errorf("Got '%v', wanted 330000000.1", val)
	}
}

func TestDouble_ConvertToNative_Json(t *testing.T) {
	val, err := Double(-1.4).ConvertToNative(jsonValueType)
	pbVal := &structpb.Value{Kind: &structpb.Value_NumberValue{NumberValue: -1.4}}
	if err != nil {
		t.Error(err)
	} else if !proto.Equal(val.(proto.Message), pbVal) {
		t.Errorf("Got '%v', expected -1.4", val)
	}
}

func TestDouble_ConvertToNative_Ptr_Float32(t *testing.T) {
	ptrType := float32(0)
	val, err := Double(3.1415).ConvertToNative(reflect.TypeOf(&ptrType))
	if err != nil {
		t.Error(err)
	} else if *val.(*float32) != 3.1415 {
		t.Errorf("Got '%v', wanted 3.1415", val)
	}
}

func TestDouble_ConvertToNative_Ptr_Float64(t *testing.T) {
	ptrType := float64(0)
	val, err := Double(30000000.1).ConvertToNative(reflect.TypeOf(&ptrType))
	if err != nil {
		t.Error(err)
	} else if *val.(*float64) != 30000000.1 {
		t.Errorf("Got '%v', wanted 330000000.1", val)
	}
}

func TestDouble_ConvertToType(t *testing.T) {
	if !Double(-4.5).ConvertToType(IntType).Equal(Int(-4)).(Bool) {
		t.Error("Unsuccessful type conversion to int")
	}
	if !Double(-4.5).ConvertToType(UintType).Equal(Uint(18446744073709551612)).(Bool) {
		t.Error("Unsuccessful type conversion to uint")
	}
	if !Double(-4.5).ConvertToType(DoubleType).Equal(Double(-4.5)).(Bool) {
		t.Error("Unsuccessful type conversion to double")
	}
	if !Double(-4.5).ConvertToType(StringType).Equal(String("-4.5")).(Bool) {
		t.Error("Unsuccessful type conversion to string")
	}
	if !Double(-4.5).ConvertToType(TypeType).Equal(DoubleType).(Bool) {
		t.Error("Unsuccessful type conversion to type")
	}
	if !IsError(Double(-4.5).ConvertToType(TimestampType)) {
		t.Error("Got value, expected error")
	}
}

func TestDouble_Divide(t *testing.T) {
	if !Double(3).Divide(Double(1.5)).Equal(Double(2)).(Bool) {
		t.Error("Dividing two doubles did not match expectations.")
	}
	if !IsError(Double(1.1).Divide(Double(0))) {
		t.Error("Division by zero did not cause error.")
	}
	if !IsError(Double(1.1).Divide(IntNegOne)) {
		t.Error("Division permitted without express type-conversion.")
	}
}

func TestDouble_Equal(t *testing.T) {
	if !IsError(Double(0).Equal(False)) {
		t.Error("Double equal to non-double resulted in non-error.")
	}
}

func TestDouble_Multiply(t *testing.T) {
	if !Double(1.1).Multiply(Double(-1.2)).Equal(Double(-1.32)).(Bool) {
		t.Error("Multiplying two doubles did not match expectations.")
	}
	if !IsError(Double(1.1).Multiply(IntNegOne)) {
		t.Error("Multiplication permitted without express type-conversion.")
	}
}

func TestDouble_Negate(t *testing.T) {
	if !Double(1.1).Negate().Equal(Double(-1.1)).(Bool) {
		t.Error("Negating double value did not succeed")
	}
}

func TestDouble_Subtract(t *testing.T) {
	if !Double(4).Subtract(Double(-3.5)).Equal(Double(7.5)).(Bool) {
		t.Error("Subtracting two doubles did not match expected value.")
	}
	if !IsError(Double(1.1).Subtract(IntNegOne)) {
		t.Error("Subtraction permitted without express type-conversion.")
	}
}
