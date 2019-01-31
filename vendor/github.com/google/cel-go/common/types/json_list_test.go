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
	structpb "github.com/golang/protobuf/ptypes/struct"

	anypb "github.com/golang/protobuf/ptypes/any"
)

func TestJsonListValue_Add(t *testing.T) {
	listA := NewJSONList(&structpb.ListValue{Values: []*structpb.Value{
		{Kind: &structpb.Value_StringValue{StringValue: "hello"}},
		{Kind: &structpb.Value_NumberValue{NumberValue: 1}}}})
	listB := NewJSONList(&structpb.ListValue{Values: []*structpb.Value{
		{Kind: &structpb.Value_NumberValue{NumberValue: 2}},
		{Kind: &structpb.Value_NumberValue{NumberValue: 3}}}})
	list := listA.Add(listB)
	nativeVal, err := list.ConvertToNative(jsonListValueType)
	if err != nil {
		t.Error(err)
	}
	expected := &structpb.ListValue{Values: []*structpb.Value{
		{Kind: &structpb.Value_StringValue{StringValue: "hello"}},
		{Kind: &structpb.Value_NumberValue{NumberValue: 1}},
		{Kind: &structpb.Value_NumberValue{NumberValue: 2}},
		{Kind: &structpb.Value_NumberValue{NumberValue: 3}}}}
	if !proto.Equal(nativeVal.(proto.Message), expected) {
		t.Errorf("Concatenated lists did not combine as expected."+
			" Got '%v', expected '%v'", nativeVal, expected)
	}
}

func TestJsonListValue_Contains(t *testing.T) {
	list := NewJSONList(&structpb.ListValue{Values: []*structpb.Value{
		{Kind: &structpb.Value_StringValue{StringValue: "hello"}},
		{Kind: &structpb.Value_NumberValue{NumberValue: 1}}}})
	if !list.Contains(Double(1)).(Bool) {
		t.Error("Expected value list to contain number '1'", list)
	}
	if list.Contains(Double(2)).(Bool) {
		t.Error("Expected value list to not contain number '2'", list)
	}
}

func TestJsonListValue_ConvertToNative_Json(t *testing.T) {
	list := NewJSONList(&structpb.ListValue{Values: []*structpb.Value{
		{Kind: &structpb.Value_StringValue{StringValue: "hello"}},
		{Kind: &structpb.Value_NumberValue{NumberValue: 1}}}})
	listVal, err := list.ConvertToNative(jsonListValueType)
	if err != nil {
		t.Error(err)
	}
	if listVal != list.Value().(proto.Message) {
		t.Error("List did not convert to its underlying representation.")
	}

	val, err := list.ConvertToNative(jsonValueType)
	if err != nil {
		t.Error(err)
	}
	if !proto.Equal(val.(proto.Message),
		&structpb.Value{Kind: &structpb.Value_ListValue{
			ListValue: listVal.(*structpb.ListValue)}}) {
		t.Errorf("Messages were not equal, got '%v'", val)
	}
}

func TestJsonListValue_ConvertToNative_Slice(t *testing.T) {
	list := NewJSONList(&structpb.ListValue{Values: []*structpb.Value{
		{Kind: &structpb.Value_StringValue{StringValue: "hello"}},
		{Kind: &structpb.Value_NumberValue{NumberValue: 1}}}})
	listVal, err := list.ConvertToNative(reflect.TypeOf([]*structpb.Value{}))
	if err != nil {
		t.Error(err)
	}
	for i, v := range listVal.([]*structpb.Value) {
		if !list.Get(Int(i)).Equal(NativeToValue(v)).(Bool) {
			t.Errorf("elem[%d] Got '%v', expected '%v'",
				i, v, list.Get(Int(i)))
		}
	}
}

func TestJsonListValue_ConvertToNative_Any(t *testing.T) {
	list := NewJSONList(&structpb.ListValue{Values: []*structpb.Value{
		{Kind: &structpb.Value_StringValue{StringValue: "hello"}},
		{Kind: &structpb.Value_NumberValue{NumberValue: 1}}}})
	anyVal, err := list.ConvertToNative(anyValueType)
	if err != nil {
		t.Error(err)
	}
	unpackedAny := ptypes.DynamicAny{}
	if ptypes.UnmarshalAny(anyVal.(*anypb.Any), &unpackedAny) != nil {
		t.Error("Fail to unmarshal any")
	}
	if !proto.Equal(unpackedAny.Message,
		list.Value().(proto.Message)) {
		t.Errorf("Messages were not equal, got '%v'", unpackedAny.Message)
	}
}

func TestJsonListValue_ConvertToType(t *testing.T) {
	list := NewJSONList(&structpb.ListValue{Values: []*structpb.Value{
		{Kind: &structpb.Value_StringValue{StringValue: "hello"}},
		{Kind: &structpb.Value_NumberValue{NumberValue: 1}}}})
	if list.ConvertToType(TypeType) != ListType {
		t.Error("Json list type was not a list.")
	}
	if list.ConvertToType(ListType) != list {
		t.Error("Json list not convertible to itself.")
	}
	if !IsError(list.ConvertToType(MapType)) {
		t.Error("Got map, expected error.")
	}
}

func TestJsonListValue_Equal(t *testing.T) {
	listA := NewJSONList(&structpb.ListValue{Values: []*structpb.Value{
		{Kind: &structpb.Value_NumberValue{NumberValue: -3}},
		{Kind: &structpb.Value_StringValue{StringValue: "hello"}}}})
	listB := NewJSONList(&structpb.ListValue{Values: []*structpb.Value{
		{Kind: &structpb.Value_NumberValue{NumberValue: 2}},
		{Kind: &structpb.Value_StringValue{StringValue: "hello"}}}})
	if listA.Equal(listB).(Bool) || listB.Equal(listA).(Bool) {
		t.Error("Lists with different elements considered equal.")
	}
	if !listA.Equal(listA).(Bool) {
		t.Error("List was not equal to itself.")
	}
	if listA.Add(listA).Equal(listB).(Bool) {
		t.Error("Lists of different size were equal.")
	}
	if !IsError(listA.Equal(True)) {
		t.Error("Equality of different type returned non-error.")
	}
}

func TestJsonListValue_Get_OutOfRange(t *testing.T) {
	list := NewJSONList(&structpb.ListValue{Values: []*structpb.Value{
		{Kind: &structpb.Value_StringValue{StringValue: "hello"}},
		{Kind: &structpb.Value_NumberValue{NumberValue: 1}}}})
	if !IsError(list.Get(Int(-1))) {
		t.Error("Negative index did not result in error.")
	}
	if !IsError(list.Get(Int(2))) {
		t.Error("Index out of range did not result in error.")
	}
	if !IsError(list.Get(Uint(1))) {
		t.Error("Index of incorrect type did not result in error.")
	}
}

func TestJsonListValue_Iterator(t *testing.T) {
	list := NewJSONList(&structpb.ListValue{Values: []*structpb.Value{
		{Kind: &structpb.Value_StringValue{StringValue: "hello"}},
		{Kind: &structpb.Value_NumberValue{NumberValue: 1}},
		{Kind: &structpb.Value_NumberValue{NumberValue: 2}},
		{Kind: &structpb.Value_NumberValue{NumberValue: 3}}}})
	it := list.Iterator()
	for i := Int(0); it.HasNext() != False; i++ {
		v := it.Next()
		if v.Equal(list.Get(i)) != True {
			t.Errorf("elem[%d] Got '%v', expected '%v'", i, v, list.Get(i))
		}
	}

	if it.HasNext() != False {
		t.Error("Iterator indicated more elements were left")
	}
	if it.Next() != nil {
		t.Error("Calling Next() for a complete iterator resulted in a non-nil value.")
	}
}
