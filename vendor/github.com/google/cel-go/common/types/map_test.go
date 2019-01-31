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

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/google/cel-go/common/types/traits"
)

func TestBaseMap_Contains(t *testing.T) {
	mapValue := NewDynamicMap(map[string]map[int32]float32{
		"nested": {1: -1.0, 2: 2.0},
		"empty":  {}}).(traits.Mapper)
	if mapValue.Contains(String("nested")) != True {
		t.Error("Expected key 'nested' contained in map.")
	}
	if mapValue.Contains(String("unknown")) != False {
		t.Error("Expected key 'unknown' contained in map.")
	}
}

func TestBaseMap_ConvertToNative_Error(t *testing.T) {
	mapValue := NewDynamicMap(map[string]map[string]float32{
		"nested": {"1": -1.0}})
	val, err := mapValue.ConvertToNative(reflect.TypeOf(""))
	if err == nil {
		t.Errorf("Got '%v', expected error", val)
	}
}

func TestBaseMap_ConvertToNative_Json(t *testing.T) {
	mapValue := NewDynamicMap(map[string]map[string]float32{
		"nested": {"1": -1.0}})
	json, err := mapValue.ConvertToNative(jsonValueType)
	if err != nil {
		t.Error(err)
	}
	jsonTxt, err := (&jsonpb.Marshaler{}).MarshalToString(json.(proto.Message))
	if jsonTxt != "{\"nested\":{\"1\":-1}}" {
		t.Error(jsonTxt)
	}
}

func TestBaseMap_ConvertToType(t *testing.T) {
	mapValue := NewDynamicMap(map[string]string{"key": "value"})
	if mapValue.ConvertToType(MapType) != mapValue {
		t.Error("Map could not be converted to a map.")
	}
	if mapValue.ConvertToType(TypeType) != MapType {
		t.Error("Map type was not listed as a map.")
	}
	if !IsError(mapValue.ConvertToType(ListType)) {
		t.Error("Map conversion to unsupported type was not an error.")
	}
}

func TestBaseMap_Equal_True(t *testing.T) {
	mapValue := NewDynamicMap(map[string]map[int32]float32{
		"nested": {1: -1.0, 2: 2.0},
		"empty":  {}})
	if mapValue.Equal(mapValue) != True {
		t.Error("Map value was not equal to itself")
	}
	if nestedVal := mapValue.Get(String("nested")); IsError(nestedVal) {
		t.Error(nestedVal)
	} else if mapValue.Equal(nestedVal) == True ||
		nestedVal.Equal(mapValue) == True {
		t.Error("Same length, but different key names")
	}
}

func TestBaseMap_Equal_False(t *testing.T) {
	mapValue := NewDynamicMap(map[string]map[int32]float32{
		"nested": {1: -1.0, 2: 2.0},
		"empty":  {}})
	otherValue := NewDynamicMap(map[string]map[int64]float64{
		"nested": {1: -1.0, 2: 2.0, 3: 3.14},
		"empty":  {}})
	if mapValue.Equal(otherValue) != False {
		t.Error("Inequal maps were deemed equal.")
	}
}

func TestBaseMap_Get(t *testing.T) {
	mapValue := NewDynamicMap(map[string]map[int32]float32{
		"nested": {1: -1.0, 2: 2.0},
		"empty":  {}}).(traits.Mapper)
	if nestedVal := mapValue.Get(String("nested")); IsError(nestedVal) {
		t.Error(nestedVal)
	} else if floatVal := nestedVal.(traits.Indexer).Get(Int(1)); IsError(floatVal) {
		t.Error(floatVal)
	} else if floatVal.Equal(Double(-1.0)) != True {
		t.Error("Nested map access of float property not float64")
	}
}

func TestBaseMap_Iterator(t *testing.T) {
	mapValue := NewDynamicMap(map[string]map[int32]float32{
		"nested": {1: -1.0, 2: 2.0},
		"empty":  {}}).(traits.Mapper)
	it := mapValue.Iterator()
	var i = 0
	var fieldNames []interface{}
	for ; it.HasNext() == True; i++ {
		if value := mapValue.Get(it.Next()); IsError(value) {
			t.Error(value)
		} else {
			fieldNames = append(fieldNames, value)
		}
	}
	if len(fieldNames) != 2 {
		t.Errorf("Did not find the correct number of fields: %v", fieldNames)
	}
	if it.Next() != nil {
		t.Error("Iterator ran off the end of the field names")
	}
}
