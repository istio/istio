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

func TestBool_Compare(t *testing.T) {
	if False.Compare(True).(Int) != IntNegOne {
		t.Error("False was not less than true")
	}
	if True.Compare(False).(Int) != IntOne {
		t.Error("True was not greater than false")
	}
	if True.Compare(True).(Int) != IntZero {
		t.Error("True was not equal to true")
	}
	if False.Compare(False).(Int) != IntZero {
		t.Error("False was not equal to false")
	}
	if !IsError(True.Compare(Uint(0))) {
		t.Error("Was able to compare uncomparable types.")
	}
}

func TestBool_ConvertToNative_Bool(t *testing.T) {
	refType := reflect.TypeOf(true)
	val, err := True.ConvertToNative(refType)
	if err != nil {
		t.Error(err)
	} else if !val.(bool) {
		t.Error("Error during conversion to bool", val)
	}
}

func TestBool_ConvertToNative_Error(t *testing.T) {
	refType := reflect.TypeOf("")
	val, err := True.ConvertToNative(refType)
	if err == nil {
		t.Errorf("Got '%v', expected error", val)
	}
}

func TestBool_ConvertToNative_Json(t *testing.T) {
	val, err := True.ConvertToNative(jsonValueType)
	pbVal := &structpb.Value{Kind: &structpb.Value_BoolValue{BoolValue: true}}
	if err != nil {
		t.Error(err)
	} else if !proto.Equal(val.(proto.Message), pbVal) {
		t.Error("Error during conversion to json Value type", val)
	}
}

func TestBool_ConvertToNative_Ptr(t *testing.T) {
	ptrType := true
	refType := reflect.TypeOf(&ptrType)
	val, err := True.ConvertToNative(refType)
	if err != nil {
		t.Error(err)
	} else if !*val.(*bool) {
		t.Error("Error during conversion to *bool", val)
	}
}

func TestBool_ConvertToType(t *testing.T) {
	if !True.ConvertToType(StringType).Equal(String("true")).(Bool) {
		t.Error("Boolean could not be converted to string")
	}
	if True.ConvertToType(BoolType) != True {
		t.Error("Boolean could not be converted to a boolean.")
	}
	if True.ConvertToType(TypeType) != BoolType {
		t.Error("Boolean could not be converted to a type.")
	}
	if !IsError(True.ConvertToType(TimestampType)) {
		t.Error("Got value, expected error.")
	}
}

func TestBool_Equal(t *testing.T) {
	if !True.Equal(True).(Bool) {
		t.Error("True was not equal to true")
	}
	if False.Equal(True).(Bool) {
		t.Error("False was equal to true")
	}
	if !IsError(Double(0.0).Equal(False)) {
		t.Error("Cross-type equality yielded non-error value.")
	}
}

func TestBool_Negate(t *testing.T) {
	if True.Negate() != False {
		t.Error("True did not negate to false.")
	}
	if False.Negate() != True {
		t.Error("False did not negate to true")
	}
}

func TestIsBool(t *testing.T) {
	if !IsBool(True) || !IsBool(False) {
		t.Error("Boolean values did not test as boolean.")
	}
	if IsBool(String("true")) {
		t.Error("Non-boolean value tested as boolean.")
	}
}
