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

package server

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/operators"
	"github.com/google/cel-go/test"
	"github.com/google/cel-spec/tools/celrpc"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

type serverTest struct {
	client *celrpc.ConfClient
}

var (
	globals = serverTest{}
)

// TestMain performs setup for testing.
func TestMain(m *testing.M) {
	// Use a helper function to ensure we run shutdown()
	// before calling os.Exit()
	os.Exit(mainHelper(m))
}

func mainHelper(m *testing.M) int {
	client, err := celrpc.NewClientFromPath(os.Args[1])
	globals.client = client
	defer client.Shutdown()
	if err != nil {
		// testing.M doesn't have a logging method.  hmm...
		log.Fatal(err)
		return 1
	}
	return m.Run()
}

var (
	parsed = &exprpb.ParsedExpr{
		Expr: test.ExprCall(1, operators.Add,
			test.ExprLiteral(2, int64(1)),
			test.ExprLiteral(3, int64(1))),
		SourceInfo: &exprpb.SourceInfo{
			Location: "the location",
			Positions: map[int64]int32{
				1: 0,
				2: 0,
				3: 4,
			},
		},
	}
)

// TestParse tests the Parse method.
func TestParse(t *testing.T) {
	req := exprpb.ParseRequest{
		CelSource: "1 + 1",
	}
	res, err := globals.client.Parse(context.Background(), &req)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("Empty result")
	}
	if res.ParsedExpr == nil {
		t.Fatal("Empty parsed expression in result")
	}
	// Could check against 'parsed' above,
	// but the expression ids are arbitrary,
	// and explicit comparison logic is about as
	// much work as normalization would be.
	if res.ParsedExpr.Expr == nil {
		t.Fatal("Empty expression in result")
	}
	switch res.ParsedExpr.Expr.ExprKind.(type) {
	case *exprpb.Expr_CallExpr:
		c := res.ParsedExpr.Expr.GetCallExpr()
		if c.Target != nil {
			t.Error("Call has target", c)
		}
		if c.Function != "_+_" {
			t.Error("Wrong function", c)
		}
		if len(c.Args) != 2 {
			t.Error("Too many or few args", c)
		}
		for i, a := range c.Args {
			switch a.ExprKind.(type) {
			case *exprpb.Expr_ConstExpr:
				l := a.GetConstExpr()
				switch l.ConstantKind.(type) {
				case *exprpb.Constant_Int64Value:
					if l.GetInt64Value() != int64(1) {
						t.Errorf("Arg %d wrong value: %v", i, a)
					}
				default:
					t.Errorf("Arg %d not int: %v", i, a)
				}
			default:
				t.Errorf("Arg %d not literal: %v", i, a)
			}
		}
	default:
		t.Error("Wrong expression type", res.ParsedExpr.Expr)
	}
}

// TestCheck tests the Check method.
func TestCheck(t *testing.T) {
	// If TestParse() passes, it validates a good chunk
	// of the server mechanisms for data conversion, so we
	// won't be as fussy here..
	req := exprpb.CheckRequest{
		ParsedExpr: parsed,
	}
	res, err := globals.client.Check(context.Background(), &req)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("Empty result")
	}
	if res.CheckedExpr == nil {
		t.Fatal("No checked expression")
	}
	tp, present := res.CheckedExpr.TypeMap[int64(1)]
	if !present {
		t.Fatal("No type for top level expression", res)
	}
	switch tp.TypeKind.(type) {
	case *exprpb.Type_Primitive:
		if tp.GetPrimitive() != exprpb.Type_INT64 {
			t.Error("Bad top-level type", tp)
		}
	default:
		t.Error("Bad top-level type", tp)
	}
}

// TestEval tests the Eval method.
func TestEval(t *testing.T) {
	req := exprpb.EvalRequest{
		ExprKind: &exprpb.EvalRequest_ParsedExpr{parsed},
	}
	res, err := globals.client.Eval(context.Background(), &req)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Result == nil {
		t.Fatal("Nil result")
	}
	switch res.Result.Kind.(type) {
	case *exprpb.ExprValue_Value:
		v := res.Result.GetValue()
		switch v.Kind.(type) {
		case *exprpb.Value_Int64Value:
			if v.GetInt64Value() != int64(2) {
				t.Error("Wrong result for 1 + 1", v)
			}
		default:
			t.Error("Wrong result value type", v)
		}
	default:
		t.Fatal("Result not a value", res.Result)
	}
}

// TestFullUp tests Parse, Check, and Eval back-to-back.
func TestFullUp(t *testing.T) {
	preq := exprpb.ParseRequest{
		CelSource: "x + y",
	}
	pres, err := globals.client.Parse(context.Background(), &preq)
	if err != nil {
		t.Fatal(err)
	}
	parsedExpr := pres.ParsedExpr
	if parsedExpr == nil {
		t.Fatal("Empty parsed expression")
	}

	creq := exprpb.CheckRequest{
		ParsedExpr: parsedExpr,
		TypeEnv: []*exprpb.Decl{
			decls.NewIdent("x", decls.Int, nil),
			decls.NewIdent("y", decls.Int, nil),
		},
	}
	cres, err := globals.client.Check(context.Background(), &creq)
	if err != nil {
		t.Fatal(err)
	}
	if cres == nil {
		t.Fatal("Empty check result")
	}
	checkedExpr := cres.CheckedExpr
	if checkedExpr == nil {
		t.Fatal("No checked expression")
	}
	tp, present := checkedExpr.TypeMap[int64(1)]
	if !present {
		t.Fatal("No type for top level expression", cres)
	}
	switch tp.TypeKind.(type) {
	case *exprpb.Type_Primitive:
		if tp.GetPrimitive() != exprpb.Type_INT64 {
			t.Error("Bad top-level type", tp)
		}
	default:
		t.Error("Bad top-level type", tp)
	}

	ereq := exprpb.EvalRequest{
		ExprKind: &exprpb.EvalRequest_CheckedExpr{checkedExpr},
		Bindings: map[string]*exprpb.ExprValue{
			"x": exprValueInt64(1),
			"y": exprValueInt64(2),
		},
	}
	eres, err := globals.client.Eval(context.Background(), &ereq)
	if err != nil {
		t.Fatal(err)
	}
	if eres == nil || eres.Result == nil {
		t.Fatal("Nil result")
	}
	switch eres.Result.Kind.(type) {
	case *exprpb.ExprValue_Value:
		v := eres.Result.GetValue()
		switch v.Kind.(type) {
		case *exprpb.Value_Int64Value:
			if v.GetInt64Value() != int64(3) {
				t.Error("Wrong result for 1 + 2", v)
			}
		default:
			t.Error("Wrong result value type", v)
		}
	default:
		t.Fatal("Result not a value", eres.Result)
	}
}

func exprValueInt64(x int64) *exprpb.ExprValue {
	return &exprpb.ExprValue{
		Kind: &exprpb.ExprValue_Value{
			&exprpb.Value{
				Kind: &exprpb.Value_Int64Value{x},
			},
		},
	}
}

// fullPipeline parses, checks, and evaluates the CEL expression in source
// and returns the result from the Eval call.
func fullPipeline(t *testing.T, source string) (*exprpb.ParseResponse, *exprpb.CheckResponse, *exprpb.EvalResponse) {
	t.Helper()

	// Parse
	preq := exprpb.ParseRequest{
		CelSource: source,
	}
	pres, err := globals.client.Parse(context.Background(), &preq)
	if err != nil {
		t.Fatal(err)
	}
	if pres == nil {
		t.Fatal("Empty parse result")
	}
	parsedExpr := pres.ParsedExpr
	if parsedExpr == nil {
		t.Fatal("Empty parsed expression")
	}
	if parsedExpr.Expr == nil {
		t.Fatal("Empty root expression")
	}

	// Check
	creq := exprpb.CheckRequest{
		ParsedExpr: parsedExpr,
	}
	cres, err := globals.client.Check(context.Background(), &creq)
	if err != nil {
		t.Fatal(err)
	}
	if cres == nil {
		t.Fatal("Empty check result")
	}
	checkedExpr := cres.CheckedExpr
	if checkedExpr == nil {
		t.Fatal("No checked expression")
	}

	// Eval
	ereq := exprpb.EvalRequest{
		ExprKind: &exprpb.EvalRequest_CheckedExpr{checkedExpr},
	}
	eres, err := globals.client.Eval(context.Background(), &ereq)
	if err != nil {
		t.Fatal(err)
	}
	if eres == nil || eres.Result == nil {
		t.Fatal("Nil result")
	}
	return pres, cres, eres
}

// expectEvalTrue parses, checks, and evaluates the CEL expression in source
// and checks that the result is the boolean value 'true'.
func expectEvalTrue(t *testing.T, source string) {
	t.Helper()
	pres, cres, eres := fullPipeline(t, source)

	rootId := pres.ParsedExpr.Expr.Id
	topType, present := cres.CheckedExpr.TypeMap[rootId]
	if !present {
		t.Fatal("No type for top level expression", cres)
	}
	switch topType.TypeKind.(type) {
	case *exprpb.Type_Primitive:
		if topType.GetPrimitive() != exprpb.Type_BOOL {
			t.Error("Bad top-level type", topType)
		}
	default:
		t.Error("Bad top-level type", topType)
	}

	switch eres.Result.Kind.(type) {
	case *exprpb.ExprValue_Value:
		v := eres.Result.GetValue()
		switch v.Kind.(type) {
		case *exprpb.Value_BoolValue:
			if !v.GetBoolValue() {
				t.Error("Wrong result", v)
			}
		default:
			t.Error("Wrong result value type", v)
		}
	default:
		t.Fatal("Result not a value", eres.Result)
	}
}

// TestCondTrue tests true conditional behavior.
func TestCondTrue(t *testing.T) {
	expectEvalTrue(t, "(true ? 'a' : 'b') == 'a'")
}

// TestCondFalse tests false conditional behavior.
func TestCondFalse(t *testing.T) {
	expectEvalTrue(t, "(false ? 'a' : 'b') == 'b'")
}

// TestMapOrderInsignificant tests that maps with different order are equal.
func TestMapOrderInsignificant(t *testing.T) {
	expectEvalTrue(t, "{1: 'a', 2: 'b'} == {2: 'b', 1: 'a'}")
}

// FailsTestOneMetaType tests that types of different types are equal.
func FailsTestOneMetaType(t *testing.T) {
	expectEvalTrue(t, "type(type(1)) == type(type('foo'))")
}

// FailsTestTypeType tests that the meta-type is its own type.
func FailsTestTypeType(t *testing.T) {
	expectEvalTrue(t, "type(type) == type")
}

// FailsTestNullTypeName checks that the type of null is "null_type".
func FailsTestNullTypeName(t *testing.T) {
	expectEvalTrue(t, "type(null) == null_type")
}

// TestError ensures that errors are properly transmitted.
func TestError(t *testing.T) {
	_, _, eres := fullPipeline(t, "1 / 0")
	switch eres.Result.Kind.(type) {
	case *exprpb.ExprValue_Error:
		return
	}
	t.Fatal("got %v, want division by zero error", eres.Result)
}
