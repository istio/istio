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
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/google/cel-go/test"

	anypb "github.com/golang/protobuf/ptypes/any"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

func TestNewProtoObject(t *testing.T) {
	parsedExpr := &exprpb.ParsedExpr{
		SourceInfo: &exprpb.SourceInfo{
			LineOffsets: []int32{1, 2, 3}}}
	obj := NewObject(parsedExpr).(traits.Indexer)
	si := obj.Get(String("source_info")).(traits.Indexer)
	lo := si.Get(String("line_offsets")).(traits.Indexer)
	if lo.Get(Int(2)).Equal(Int(3)) != True {
		t.Errorf("Could not select fields by their proto type names")
	}
	expr := obj.Get(String("expr")).(traits.Indexer)
	call := expr.Get(String("call_expr")).(traits.Indexer)
	if call.Get(String("function")).Equal(String("")) != True {
		t.Errorf("Could not traverse through default values for unset fields")
	}
}

func TestProtoObject_Iterator(t *testing.T) {
	existsMsg := NewObject(test.Exists.Expr).(traits.Iterable)
	it := existsMsg.Iterator()
	var fields []ref.Value
	for it.HasNext() == True {
		fields = append(fields, it.Next())
	}
	if !reflect.DeepEqual(fields, []ref.Value{String("id"), String("comprehension_expr")}) {
		t.Errorf("Got %v, wanted %v", fields, []interface{}{"id", "comprehension_expr"})
	}
}

func TestProtoObj_ConvertToNative(t *testing.T) {
	pbMessage := &exprpb.ParsedExpr{
		SourceInfo: &exprpb.SourceInfo{
			LineOffsets: []int32{1, 2, 3}}}
	objVal := NewObject(pbMessage)

	// Proto Message
	val, err := objVal.ConvertToNative(reflect.TypeOf(&exprpb.ParsedExpr{}))
	if err != nil {
		t.Error(err)
	}
	if !proto.Equal(val.(proto.Message), pbMessage) {
		t.Errorf("Messages were not equal, expect '%v', got '%v'", objVal.Value(), pbMessage)
	}

	// google.protobuf.Any
	anyVal, err := objVal.ConvertToNative(anyValueType)
	if err != nil {
		t.Error(err)
	}
	unpackedAny := ptypes.DynamicAny{}
	if ptypes.UnmarshalAny(anyVal.(*anypb.Any), &unpackedAny) != nil {
		NewErr("Failed to unmarshal any")
	}
	if !proto.Equal(unpackedAny.Message, objVal.Value().(proto.Message)) {
		t.Errorf("Messages were not equal, expect '%v', got '%v'", objVal.Value(), unpackedAny.Message)
	}
}
