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

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/struct"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

func TestTypeProvider_NewValue(t *testing.T) {
	typeProvider := NewProvider(&exprpb.ParsedExpr{})
	if sourceInfo := typeProvider.NewValue(
		"google.api.expr.v1alpha1.SourceInfo",
		map[string]ref.Value{
			"location":     String("TestTypeProvider_NewValue"),
			"line_offsets": NewDynamicList([]int64{0, 2}),
			"positions":    NewDynamicMap(map[int64]int64{1: 2, 2: 4}),
		}); IsError(sourceInfo) {
		t.Error(sourceInfo)
	} else {
		info := sourceInfo.Value().(*exprpb.SourceInfo)
		if info.Location != "TestTypeProvider_NewValue" ||
			!reflect.DeepEqual(info.LineOffsets, []int32{0, 2}) ||
			!reflect.DeepEqual(info.Positions, map[int64]int32{1: 2, 2: 4}) {
			t.Errorf("Source info not properly configured: %v", info)
		}
	}
}

func TestTypeProvider_NewValue_OneofFields(t *testing.T) {
	typeProvider := NewProvider(&exprpb.ParsedExpr{})
	if exp := typeProvider.NewValue(
		"google.api.expr.v1alpha1.Expr",
		map[string]ref.Value{
			"const_expr": NewObject(&exprpb.Constant{ConstantKind: &exprpb.Constant_StringValue{StringValue: "oneof"}}),
		}); IsError(exp) {
		t.Error(exp)
	} else {
		e := exp.Value().(*exprpb.Expr)
		if e.GetConstExpr().GetStringValue() != "oneof" {
			t.Errorf("Expr with oneof could not be created: %v", e)
		}
	}
}

func TestTypeProvider_Getters(t *testing.T) {
	typeProvider := NewProvider(&exprpb.ParsedExpr{})
	if sourceInfo := typeProvider.NewValue(
		"google.api.expr.v1alpha1.SourceInfo",
		map[string]ref.Value{
			"location":     String("TestTypeProvider_GetFieldValue"),
			"line_offsets": NewDynamicList([]int64{0, 2}),
			"positions":    NewDynamicMap(map[int64]int64{1: 2, 2: 4}),
		}); IsError(sourceInfo) {
		t.Error(sourceInfo)
	} else {
		si := sourceInfo.(traits.Indexer)
		if loc := si.Get(String("location")); IsError(loc) {
			t.Error(loc)
		} else if loc.(String) != "TestTypeProvider_GetFieldValue" {
			t.Errorf("Expected %s, got %s",
				"TestTypeProvider_GetFieldValue",
				loc)
		}
		if pos := si.Get(String("positions")); IsError(pos) {
			t.Error(pos)
		} else if pos.Equal(NewDynamicMap(map[int64]int32{1: 2, 2: 4})) != True {
			t.Errorf("Expected map[int64]int32, got %v", pos)
		} else if posKeyVal := pos.(traits.Indexer).Get(Int(1)); IsError(posKeyVal) {
			t.Error(posKeyVal)
		} else if posKeyVal.(Int) != 2 {
			t.Error("Expected value to be int64, not int32")
		}
		if offsets := si.Get(String("line_offsets")); IsError(offsets) {
			t.Error(offsets)
		} else if offset1 := offsets.(traits.Lister).Get(Int(1)); IsError(offset1) {
			t.Error(offset1)
		} else if offset1.(Int) != 2 {
			t.Errorf("Expected index 1 to be value 2, was %v", offset1)
		}
	}
}

func TestValue_ConvertToNative(t *testing.T) {
	// Core type conversion tests.
	expectValueToNative(t, True, true)
	expectValueToNative(t, Int(-1), int32(-1))
	expectValueToNative(t, Int(2), int64(2))
	expectValueToNative(t, Uint(3), uint32(3))
	expectValueToNative(t, Uint(4), uint64(4))
	expectValueToNative(t, Double(5.5), float32(5.5))
	expectValueToNative(t, Double(-5.5), float64(-5.5))
	expectValueToNative(t, String("hello"), "hello")
	expectValueToNative(t, Bytes("world"), []byte("world"))
	expectValueToNative(t, NewDynamicList([]int64{1, 2, 3}), []int32{1, 2, 3})
	expectValueToNative(t, NewDynamicMap(map[int64]int64{1: 1, 2: 1, 3: 1}),
		map[int32]int32{1: 1, 2: 1, 3: 1})

	// Null conversion tests.
	expectValueToNative(t, Null(structpb.NullValue_NULL_VALUE), structpb.NullValue_NULL_VALUE)

	// Proto conversion tests.
	parsedExpr := &exprpb.ParsedExpr{}
	expectValueToNative(t, NewObject(parsedExpr), parsedExpr)
}

func TestNativeToValue_Any(t *testing.T) {
	// NullValue
	anyValue, err := NullValue.ConvertToNative(anyValueType)
	if err != nil {
		t.Error(err)
	}
	expectNativeToValue(t, anyValue, NullValue)

	// Json Struct
	anyValue, err = ptypes.MarshalAny(&structpb.Value{
		Kind: &structpb.Value_StructValue{
			StructValue: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"a": {Kind: &structpb.Value_StringValue{StringValue: "world"}},
					"b": {Kind: &structpb.Value_StringValue{StringValue: "five!"}}}}}})
	if err != nil {
		t.Error(err)
	}
	expected := NewJSONStruct(&structpb.Struct{
		Fields: map[string]*structpb.Value{
			"a": {Kind: &structpb.Value_StringValue{StringValue: "world"}},
			"b": {Kind: &structpb.Value_StringValue{StringValue: "five!"}}}})
	expectNativeToValue(t, anyValue, expected)

	//Json List
	anyValue, err = ptypes.MarshalAny(&structpb.Value{
		Kind: &structpb.Value_ListValue{
			ListValue: &structpb.ListValue{
				Values: []*structpb.Value{
					{Kind: &structpb.Value_StringValue{StringValue: "world"}},
					{Kind: &structpb.Value_StringValue{StringValue: "five!"}}}}}})
	if err != nil {
		t.Error(err)
	}
	expected = NewJSONList(&structpb.ListValue{
		Values: []*structpb.Value{
			{Kind: &structpb.Value_StringValue{StringValue: "world"}},
			{Kind: &structpb.Value_StringValue{StringValue: "five!"}}}})
	expectNativeToValue(t, anyValue, expected)

	// Object
	pbMessage := exprpb.ParsedExpr{
		SourceInfo: &exprpb.SourceInfo{
			LineOffsets: []int32{1, 2, 3}}}
	anyValue, err = ptypes.MarshalAny(&pbMessage)
	if err != nil {
		t.Error(err)
	}
	expectNativeToValue(t, anyValue, NewObject(&pbMessage))
}

func TestNativeToValue_Json(t *testing.T) {
	// Json primitive conversion test.
	expectNativeToValue(t,
		&structpb.Value{Kind: &structpb.Value_BoolValue{}},
		False)
	expectNativeToValue(t,
		&structpb.Value{Kind: &structpb.Value_NumberValue{NumberValue: 1.1}},
		Double(1.1))
	expectNativeToValue(t,
		&structpb.Value{Kind: &structpb.Value_NullValue{
			NullValue: structpb.NullValue_NULL_VALUE}},
		Null(structpb.NullValue_NULL_VALUE))
	expectNativeToValue(t,
		&structpb.Value{Kind: &structpb.Value_StringValue{StringValue: "hello"}},
		String("hello"))

	// Json list conversion.
	expectNativeToValue(t,
		&structpb.Value{
			Kind: &structpb.Value_ListValue{
				ListValue: &structpb.ListValue{
					Values: []*structpb.Value{
						{Kind: &structpb.Value_StringValue{StringValue: "world"}},
						{Kind: &structpb.Value_StringValue{StringValue: "five!"}}}}}},
		NewJSONList(&structpb.ListValue{
			Values: []*structpb.Value{
				{Kind: &structpb.Value_StringValue{StringValue: "world"}},
				{Kind: &structpb.Value_StringValue{StringValue: "five!"}}}}))

	// Json struct conversion.
	expectNativeToValue(t,
		&structpb.Value{
			Kind: &structpb.Value_StructValue{
				StructValue: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"a": {Kind: &structpb.Value_StringValue{StringValue: "world"}},
						"b": {Kind: &structpb.Value_StringValue{StringValue: "five!"}}}}}},
		NewJSONStruct(&structpb.Struct{
			Fields: map[string]*structpb.Value{
				"a": {Kind: &structpb.Value_StringValue{StringValue: "world"}},
				"b": {Kind: &structpb.Value_StringValue{StringValue: "five!"}}}}))

	// Proto conversion test.
	parsedExpr := &exprpb.ParsedExpr{}
	expectNativeToValue(t, parsedExpr, NewObject(parsedExpr))
}

func TestNativeToValue_Primitive(t *testing.T) {
	// Core type conversions.
	expectNativeToValue(t, true, True)
	expectNativeToValue(t, int32(-1), Int(-1))
	expectNativeToValue(t, int64(2), Int(2))
	expectNativeToValue(t, uint32(3), Uint(3))
	expectNativeToValue(t, uint64(4), Uint(4))
	expectNativeToValue(t, float32(5.5), Double(5.5))
	expectNativeToValue(t, float64(-5.5), Double(-5.5))
	expectNativeToValue(t, "hello", String("hello"))
	expectNativeToValue(t, []byte("world"), Bytes("world"))
	expectNativeToValue(t, []int32{1, 2, 3}, NewDynamicList([]int32{1, 2, 3}))
	expectNativeToValue(t, map[int32]int32{1: 1, 2: 1, 3: 1},
		NewDynamicMap(map[int32]int32{1: 1, 2: 1, 3: 1}))
	// Null conversion test.
	expectNativeToValue(t, structpb.NullValue_NULL_VALUE, Null(structpb.NullValue_NULL_VALUE))
}

func TestUnsupportedConversion(t *testing.T) {
	if val := NativeToValue(nonConvertible{}); !IsError(val) {
		t.Error("Expected error when converting non-proto struct to proto", val)
	}
}

func expectValueToNative(t *testing.T, in ref.Value, out interface{}) {
	t.Helper()
	if val, err := in.ConvertToNative(reflect.TypeOf(out)); err != nil {
		t.Error(err)
	} else {
		var equals bool
		switch val.(type) {
		case []byte:
			equals = bytes.Equal(val.([]byte), out.([]byte))
		case proto.Message:
			equals = proto.Equal(val.(proto.Message), out.(proto.Message))
		case bool, int32, int64, uint32, uint64, float32, float64, string:
			equals = val == out
		default:
			equals = reflect.DeepEqual(val, out)
		}
		if !equals {
			t.Errorf("Unexpected conversion from expr to proto.\n"+
				"expected: %T, actual: %T", val, out)
		}
	}
}

func expectNativeToValue(t *testing.T, in interface{}, out ref.Value) {
	t.Helper()
	if val := NativeToValue(in); IsError(val) {
		t.Error(val)
	} else {
		if val.Equal(out) != True {
			t.Errorf("Unexpected conversion from expr to proto.\n"+
				"expected: %T, actual: %T", val, out)
		}
	}
}

type nonConvertible struct {
	Field string
}

func BenchmarkTypeProvider_NewValue(b *testing.B) {
	typeProvider := NewProvider(&exprpb.ParsedExpr{})
	for i := 0; i < b.N; i++ {
		typeProvider.NewValue(
			"google.api.expr.v1.SourceInfo",
			map[string]ref.Value{
				"Location":    String("BenchmarkTypeProvider_NewValue"),
				"LineOffsets": NewDynamicList([]int64{0, 2}),
				"Positions":   NewDynamicMap(map[int64]int64{1: 2, 2: 4}),
			})
	}
}
