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

package interpreter

import (
	"testing"

	"github.com/google/cel-go/common/debug"
	"github.com/google/cel-go/common/operators"
	"github.com/google/cel-go/test"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

type testInfo struct {
	E *exprpb.Expr
	P string
}

var testCases = []testInfo{
	{
		E: test.ExprCall(2, operators.LogicalAnd,
			test.ExprLiteral(1, true),
			test.ExprLiteral(3, false)),
		P: `false`,
	},
	{
		E: test.ExprCall(4, operators.LogicalAnd,
			test.ExprCall(2, operators.LogicalOr,
				test.ExprLiteral(1, true),
				test.ExprLiteral(3, false)),
			test.ExprIdent(5, "x")),
		P: `x`,
	},
	{
		E: test.ExprCall(4, operators.LogicalAnd,
			test.ExprCall(2, operators.LogicalOr,
				test.ExprLiteral(1, false),
				test.ExprLiteral(3, false)),
			test.ExprIdent(5, "x")),
		P: `false`,
	},
	{
		E: test.ExprCall(2, operators.LogicalAnd,
			test.ExprIdent(1, "a"),
			test.ExprComprehension(3,
				"x",
				test.ExprList(7,
					test.ExprLiteral(4, int64(1)),
					test.ExprLiteral(5, uint64(1)),
					test.ExprLiteral(6, float64(1.0))),
				"__result__",
				test.ExprLiteral(8, false),
				test.ExprCall(11,
					operators.NotStrictlyFalse,
					test.ExprCall(9,
						operators.LogicalNot,
						test.ExprIdent(10, "__result__"))),
				test.ExprCall(12,
					operators.LogicalOr,
					test.ExprIdent(13, "__result__"),
					test.ExprCall(14,
						operators.Equals,
						test.ExprCall(15,
							"type",
							test.ExprIdent(16, "x")),
						test.ExprIdent(17, "uint"))),
				test.ExprIdent(18, "__result__"))),
		P: `a`,
	},
	{
		E: test.ExprMap(8,
			test.ExprEntry(2,
				test.ExprLiteral(1, "hello"),
				test.ExprMemberCall(3,
					"size",
					test.ExprLiteral(4, "world"))),
			test.ExprEntry(6,
				test.ExprLiteral(5, "bytes"),
				test.ExprLiteral(7, []byte("bytes-string")))),
		P: `{"hello":5, "bytes":b"bytes-string"}`,
	},
	{
		E: test.ExprCall(1, operators.Less,
			test.ExprLiteral(2, int64(2)),
			test.ExprLiteral(3, int64(3))),
		P: `true`,
	},
	{
		E: test.ExprCall(8, operators.Conditional,
			test.ExprLiteral(1, true),
			test.ExprCall(3,
				operators.Less,
				test.ExprIdent(2, "b"),
				test.ExprLiteral(4, 1.2)),
			test.ExprCall(6,
				operators.Equals,
				test.ExprIdent(5, "c"),
				test.ExprList(7, test.ExprLiteral(7, "hello")))),
		P: `_<_(b,1.2)`,
	},
}

func TestPrune(t *testing.T) {
	for i, tst := range testCases {
		pExpr := &exprpb.ParsedExpr{Expr: tst.E}
		state := NewEvalState()
		interpretable, _ := interpreter.NewUncheckedInterpretable(
			pExpr.Expr,
			ExhaustiveEval(state))
		interpretable.Eval(NewActivation(map[string]interface{}{}))
		newExpr := PruneAst(pExpr.Expr, state)
		actual := debug.ToDebugString(newExpr)
		if !test.Compare(actual, tst.P) {
			t.Fatalf("prune[%d], diff: %s", i, test.DiffMessage("structure", actual, tst.P))
		}
	}
}
