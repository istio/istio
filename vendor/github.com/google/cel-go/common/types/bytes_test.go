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
	"bytes"
	"reflect"
	"testing"
)

func TestBytes_Add(t *testing.T) {
	if !Bytes("hello").Add(Bytes("world")).Equal(Bytes("helloworld")).(Bool) {
		t.Error("Byte ranges were not successfully added.")
	}
	if !IsError(Bytes("hello").Add(String("world"))) {
		t.Error("Types combined without conversion.")
	}
}

func TestBytes_Compare(t *testing.T) {
	if !Bytes("1234").Compare(Bytes("2345")).Equal(IntNegOne).(Bool) {
		t.Error("Comparison did not yield -1")
	}
	if !Bytes("2345").Compare(Bytes("1234")).Equal(IntOne).(Bool) {
		t.Error("Comparison did not yield 1")
	}
	if !Bytes("2345").Compare(Bytes("2345")).Equal(IntZero).(Bool) {
		t.Error("Comparison did not yield 0")
	}
	if !IsError(Bytes("1").Compare(String("1"))) {
		t.Error("Comparison permitted without type conversion")
	}
}

func TestBytes_ConvertToNative_ByteSlice(t *testing.T) {
	val, err := Bytes("123").ConvertToNative(reflect.TypeOf([]byte{}))
	if err != nil || !bytes.Equal(val.([]byte), []byte{49, 50, 51}) {
		t.Error("Got unexpected value, wanted []byte{49, 50, 51}", err, val)
	}
}

func TestBytes_ConvertToNative_Error(t *testing.T) {
	val, err := Bytes("123").ConvertToNative(reflect.TypeOf(""))
	if err == nil {
		t.Errorf("Got '%v', expected error", val)
	}
}

func TestBytes_ConvertToType(t *testing.T) {
	if !Bytes("hello world").ConvertToType(BytesType).Equal(Bytes("hello world")).(Bool) {
		t.Error("Unsupported type conversion to bytes")
	}
	if !Bytes("hello world").ConvertToType(StringType).Equal(String("hello world")).(Bool) {
		t.Error("Unsupported type conversion to string")
	}
	if !Bytes("hello world").ConvertToType(TypeType).Equal(BytesType).(Bool) {
		t.Error("Unsupported type conversion to type")
	}
	if !IsError(Bytes("hello").ConvertToType(IntType)) {
		t.Errorf("Got value, expected error")
	}
}

func TestBytes_Size(t *testing.T) {
	if !Bytes("1234567890").Size().Equal(Int(10)).(Bool) {
		t.Error("Unexpected byte count.")
	}
}
