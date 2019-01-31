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

package test

import (
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/struct"
	"github.com/google/cel-go/common/operators"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// TestExpr packages an Expr with SourceInfo, for testing.
type TestExpr struct {
	Expr       *exprpb.Expr
	SourceInfo *exprpb.SourceInfo
}

// Info returns a copy of the SourceInfo with the given location.
func (t *TestExpr) Info(location string) *exprpb.SourceInfo {
	info := proto.Clone(t.SourceInfo).(*exprpb.SourceInfo)
	info.Location = location
	return info
}

var (
	// Empty generates a program with no instructions.
	Empty = &TestExpr{
		Expr: &exprpb.Expr{},

		SourceInfo: &exprpb.SourceInfo{
			LineOffsets: []int32{},
			Positions:   map[int64]int32{}}}

	// Exists generates "[1, 1u, 1.0].exists(x, type(x) == uint)".
	Exists = &TestExpr{
		Expr: ExprComprehension(1,
			"x",
			ExprList(8,
				ExprLiteral(2, int64(0)),
				ExprLiteral(3, int64(1)),
				ExprLiteral(4, int64(2)),
				ExprLiteral(5, int64(3)),
				ExprLiteral(6, int64(4)),
				ExprLiteral(7, uint64(5))),
			"__result__",
			ExprLiteral(9, false),
			ExprCall(12,
				operators.NotStrictlyFalse,
				ExprCall(10,
					operators.LogicalNot,
					ExprIdent(11, "__result__"))),
			ExprCall(13,
				operators.LogicalOr,
				ExprIdent(14, "__result__"),
				ExprCall(15,
					operators.Equals,
					ExprCall(16,
						"type",
						ExprIdent(17, "x")),
					ExprIdent(18, "uint"))),
			ExprIdent(19, "__result__")),

		SourceInfo: &exprpb.SourceInfo{
			LineOffsets: []int32{0},
			Positions: map[int64]int32{
				0:  12,
				1:  0,
				2:  1,
				3:  4,
				4:  8,
				5:  0,
				6:  18,
				7:  18,
				8:  18,
				9:  18,
				10: 18,
				11: 20,
				12: 20,
				13: 28,
				14: 28,
				15: 28,
				16: 28,
				17: 28,
				18: 28,
				19: 28}}}

	// ExistsWithInput generates "elems.exists(x, type(x) == uint)".
	ExistsWithInput = &TestExpr{
		Expr: ExprComprehension(1,
			"x",
			ExprIdent(2, "elems"),
			"__result__",
			ExprLiteral(3, false),
			ExprCall(4,
				operators.LogicalNot,
				ExprIdent(5, "__result__")),
			ExprCall(6,
				operators.Equals,
				ExprCall(7,
					"type",
					ExprIdent(8, "x")),
				ExprIdent(9, "uint")),
			ExprIdent(10, "__result__")),

		SourceInfo: &exprpb.SourceInfo{
			LineOffsets: []int32{0},
			Positions: map[int64]int32{
				0:  12,
				1:  0,
				2:  1,
				3:  4,
				4:  8,
				5:  0,
				6:  18,
				7:  18,
				8:  18,
				9:  18,
				10: 18}}}

	// DynMap generates a map literal:
	// {"hello": "world".size(),
	//  "dur": duration.Duration{10},
	//  "ts": timestamp.Timestamp{1000},
	//  "null": null,
	//  "bytes": b"bytes-string"}
	DynMap = &TestExpr{
		Expr: ExprMap(17,
			ExprEntry(2,
				ExprLiteral(1, "hello"),
				ExprMemberCall(3,
					"size",
					ExprLiteral(4, "world"))),
			ExprEntry(12,
				ExprLiteral(11, "null"),
				ExprLiteral(13, structpb.NullValue_NULL_VALUE)),
			ExprEntry(15,
				ExprLiteral(14, "bytes"),
				ExprLiteral(16, []byte("bytes-string")))),

		SourceInfo: &exprpb.SourceInfo{
			LineOffsets: []int32{},
			Positions:   map[int64]int32{}}}

	// LogicalAnd generates "a && {c: true}.c".
	LogicalAnd = &TestExpr{
		ExprCall(2, operators.LogicalAnd,
			ExprIdent(1, "a"),
			ExprSelect(8,
				ExprMap(5,
					ExprEntry(4, ExprLiteral(6, "c"), ExprLiteral(7, true))),
				"c")),
		&exprpb.SourceInfo{
			LineOffsets: []int32{},
			Positions:   map[int64]int32{}}}

	// LogicalOr generates "{c: false}.c || a".
	LogicalOr = &TestExpr{
		ExprCall(2, operators.LogicalOr,
			ExprSelect(8,
				ExprMap(5,
					ExprEntry(4, ExprLiteral(6, "c"), ExprLiteral(7, false))),
				"c"),
			ExprIdent(1, "a")),
		&exprpb.SourceInfo{
			LineOffsets: []int32{},
			Positions:   map[int64]int32{}}}

	// LogicalOrEquals generates "a || b == 'b'".
	LogicalOrEquals = &TestExpr{
		ExprCall(5, operators.LogicalOr,
			ExprIdent(1, "a"),
			ExprCall(4, operators.Equals,
				ExprIdent(2, "b"),
				ExprLiteral(3, "b"))),
		&exprpb.SourceInfo{
			LineOffsets: []int32{},
			Positions:   map[int64]int32{}}}

	// LogicalAndMissingType generates "a && TestProto{c: true}.c" where the
	// type 'TestProto' is undefined.
	LogicalAndMissingType = &TestExpr{
		ExprCall(2, operators.LogicalAnd,
			ExprIdent(1, "a"),
			ExprSelect(7,
				ExprType(5, "TestProto",
					ExprField(4, "c", ExprLiteral(6, true))),
				"c")),
		&exprpb.SourceInfo{
			LineOffsets: []int32{},
			Positions:   map[int64]int32{}}}

	// Conditional generates "a ? b < 1.0 : c == ["hello"]".
	Conditional = &TestExpr{
		Expr: ExprCall(9, operators.Conditional,
			ExprIdent(1, "a"),
			ExprCall(3,
				operators.Less,
				ExprIdent(2, "b"),
				ExprLiteral(4, 1.0)),
			ExprCall(6,
				operators.Equals,
				ExprIdent(5, "c"),
				ExprList(8, ExprLiteral(7, "hello")))),
		SourceInfo: &exprpb.SourceInfo{
			LineOffsets: []int32{},
			Positions:   map[int64]int32{}}}

	// Select generates "a.b.c".
	Select = &TestExpr{
		Expr: ExprSelect(3,
			ExprSelect(2,
				ExprIdent(1, "a"),
				"b"),
			"c"),
		SourceInfo: &exprpb.SourceInfo{
			LineOffsets: []int32{},
			Positions:   map[int64]int32{}}}

	// Equality generates "a == 42".
	Equality = &TestExpr{
		Expr: ExprCall(2,
			operators.Equals,
			ExprIdent(1, "a"),
			ExprLiteral(3, int64(42))),
		SourceInfo: &exprpb.SourceInfo{
			LineOffsets: []int32{},
			Positions:   map[int64]int32{}}}

	// TypeEquality generates "type(a) == uint".
	TypeEquality = &TestExpr{
		Expr: ExprCall(4,
			operators.Equals,
			ExprCall(1, "type",
				ExprIdent(2, "a")),
			ExprIdent(3, "uint")),
		SourceInfo: &exprpb.SourceInfo{
			LineOffsets: []int32{},
			Positions:   map[int64]int32{}}}
)

// ExprIdent creates an ident (variable) Expr.
func ExprIdent(id int64, name string) *exprpb.Expr {
	return &exprpb.Expr{Id: id, ExprKind: &exprpb.Expr_IdentExpr{
		IdentExpr: &exprpb.Expr_Ident{Name: name}}}
}

// ExprSelect creates a select Expr.
func ExprSelect(id int64, operand *exprpb.Expr, field string) *exprpb.Expr {
	return &exprpb.Expr{Id: id,
		ExprKind: &exprpb.Expr_SelectExpr{
			SelectExpr: &exprpb.Expr_Select{
				Operand:  operand,
				Field:    field,
				TestOnly: false}}}
}

// ExprLiteral creates a literal (constant) Expr.
func ExprLiteral(id int64, value interface{}) *exprpb.Expr {
	var literal *exprpb.Constant
	switch value.(type) {
	case bool:
		literal = &exprpb.Constant{ConstantKind: &exprpb.Constant_BoolValue{
			BoolValue: value.(bool)}}
	case int64:
		literal = &exprpb.Constant{ConstantKind: &exprpb.Constant_Int64Value{
			Int64Value: value.(int64)}}
	case uint64:
		literal = &exprpb.Constant{ConstantKind: &exprpb.Constant_Uint64Value{
			Uint64Value: value.(uint64)}}
	case float64:
		literal = &exprpb.Constant{ConstantKind: &exprpb.Constant_DoubleValue{
			DoubleValue: value.(float64)}}
	case string:
		literal = &exprpb.Constant{ConstantKind: &exprpb.Constant_StringValue{
			StringValue: value.(string)}}
	case structpb.NullValue:
		literal = &exprpb.Constant{ConstantKind: &exprpb.Constant_NullValue{
			NullValue: value.(structpb.NullValue)}}
	case []byte:
		literal = &exprpb.Constant{ConstantKind: &exprpb.Constant_BytesValue{
			BytesValue: value.([]byte)}}
	default:
		panic("literal type not implemented")
	}
	return &exprpb.Expr{Id: id, ExprKind: &exprpb.Expr_ConstExpr{ConstExpr: literal}}
}

// ExprCall creates a call Expr.
func ExprCall(id int64, function string, args ...*exprpb.Expr) *exprpb.Expr {
	return &exprpb.Expr{Id: id,
		ExprKind: &exprpb.Expr_CallExpr{
			CallExpr: &exprpb.Expr_Call{Target: nil, Function: function, Args: args}}}
}

// ExprMemberCall creates a receiver-style call Expr.
func ExprMemberCall(id int64, function string, target *exprpb.Expr, args ...*exprpb.Expr) *exprpb.Expr {
	return &exprpb.Expr{Id: id,
		ExprKind: &exprpb.Expr_CallExpr{
			CallExpr: &exprpb.Expr_Call{Target: target, Function: function, Args: args}}}
}

// ExprList creates a create list Expr.
func ExprList(id int64, elements ...*exprpb.Expr) *exprpb.Expr {
	return &exprpb.Expr{Id: id,
		ExprKind: &exprpb.Expr_ListExpr{
			ListExpr: &exprpb.Expr_CreateList{Elements: elements}}}
}

// ExprMap creates a create struct Expr for a map.
func ExprMap(id int64, entries ...*exprpb.Expr_CreateStruct_Entry) *exprpb.Expr {
	return &exprpb.Expr{Id: id, ExprKind: &exprpb.Expr_StructExpr{
		StructExpr: &exprpb.Expr_CreateStruct{Entries: entries}}}
}

// ExprType creates creates a create struct Expr for a message.
func ExprType(id int64, messageName string,
	entries ...*exprpb.Expr_CreateStruct_Entry) *exprpb.Expr {
	return &exprpb.Expr{Id: id, ExprKind: &exprpb.Expr_StructExpr{
		StructExpr: &exprpb.Expr_CreateStruct{
			MessageName: messageName, Entries: entries}}}
}

// ExprEntry creates a map entry for a create struct Expr.
func ExprEntry(id int64, key *exprpb.Expr,
	value *exprpb.Expr) *exprpb.Expr_CreateStruct_Entry {
	return &exprpb.Expr_CreateStruct_Entry{Id: id,
		KeyKind: &exprpb.Expr_CreateStruct_Entry_MapKey{MapKey: key},
		Value:   value}
}

// ExprField creates a field entry for a create struct Expr.
func ExprField(id int64, field string,
	value *exprpb.Expr) *exprpb.Expr_CreateStruct_Entry {
	return &exprpb.Expr_CreateStruct_Entry{Id: id,
		KeyKind: &exprpb.Expr_CreateStruct_Entry_FieldKey{FieldKey: field},
		Value:   value}
}

// ExprComprehension returns a comprehension Expr.
func ExprComprehension(id int64,
	iterVar string, iterRange *exprpb.Expr,
	accuVar string, accuInit *exprpb.Expr,
	loopCondition *exprpb.Expr, loopStep *exprpb.Expr,
	resultExpr *exprpb.Expr) *exprpb.Expr {
	return &exprpb.Expr{Id: id,
		ExprKind: &exprpb.Expr_ComprehensionExpr{
			ComprehensionExpr: &exprpb.Expr_Comprehension{
				IterVar:       iterVar,
				IterRange:     iterRange,
				AccuVar:       accuVar,
				AccuInit:      accuInit,
				LoopCondition: loopCondition,
				LoopStep:      loopStep,
				Result:        resultExpr}}}
}
