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
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/struct"

	anypb "github.com/golang/protobuf/ptypes/any"
)

func TestNull_ConvertToNative(t *testing.T) {
	expected := &structpb.Value{
		Kind: &structpb.Value_NullValue{
			NullValue: structpb.NullValue_NULL_VALUE}}

	// Json Value
	val, err := NullValue.ConvertToNative(jsonValueType)
	if err != nil {
		t.Error("Fail to convert Null to jsonValueType")
	}
	if !proto.Equal(expected, val.(proto.Message)) {
		t.Errorf("Messages were not equal, got '%v'", val)
	}

	// google.protobuf.Any
	val, err = NullValue.ConvertToNative(anyValueType)
	if err != nil {
		t.Error("Fail to convert Null to any.")
	}
	data := ptypes.DynamicAny{}
	if ptypes.UnmarshalAny(val.(*anypb.Any), &data) != nil {
		t.Error("Fail to unmarshal any.")
	}
	if !proto.Equal(expected, data.Message) {
		t.Errorf("Messages were not equal, got '%v'", data.Message)
	}

	// NullValue
	val, err = NullValue.ConvertToNative(reflect.TypeOf(structpb.NullValue_NULL_VALUE))
	if err != nil {
		t.Error("Fail to convert Null to strcutpb.NullValue")
	}
	if val != structpb.NullValue_NULL_VALUE {
		t.Errorf("Messages were not equal, got '%v'", val)
	}
}

func TestNull_ConvertToType(t *testing.T) {
	if !NullValue.ConvertToType(NullType).Equal(NullValue).(Bool) {
		t.Error("Fail to get NullType of NullValue.")
	}

	if !NullValue.ConvertToType(StringType).Equal(String("null")).(Bool) {
		t.Error("Fail to get StringType of NullValue.")
	}
}

func TestNull_Equal(t *testing.T) {
	if !NullValue.Equal(NullValue).(Bool) {
		t.Error("NullValue does not equal to itself.")
	}
}

func TestNull_Type(t *testing.T) {
	if NullValue.Type() != NullType {
		t.Error("NullValue gets incorrect type.")
	}
}

func TestNull_Value(t *testing.T) {
	if NullValue.Value() != structpb.NullValue_NULL_VALUE {
		t.Error("NullValue gets incorrect value.")
	}
}
