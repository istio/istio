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

package parser

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/debug"
	"github.com/google/cel-go/test"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

var testCases = []testInfo{
	{
		I: `"A"`,
		P: `"A"^#1:*expr.Constant_StringValue#`,
	},
	{
		I: `true`,
		P: `true^#1:*expr.Constant_BoolValue#`,
	},
	{
		I: `false`,
		P: `false^#1:*expr.Constant_BoolValue#`,
	},
	{
		I: `0`,
		P: `0^#1:*expr.Constant_Int64Value#`,
	},
	{
		I: `42`,
		P: `42^#1:*expr.Constant_Int64Value#`,
	},
	{
		I: `0u`,
		P: `0u^#1:*expr.Constant_Uint64Value#`,
	},
	{
		I: `23u`,
		P: `23u^#1:*expr.Constant_Uint64Value#`,
	},
	{
		I: `24u`,
		P: `24u^#1:*expr.Constant_Uint64Value#`,
	},
	{
		I: `-1`,
		P: `-1^#1:*expr.Constant_Int64Value#`,
	},
	{
		I: `4--4`,
		P: `_-_(
			4^#1:*expr.Constant_Int64Value#,
			-4^#2:*expr.Constant_Int64Value#
		  )^#3:*expr.Expr_CallExpr#`,
	},
	{
		I: `4--4.1`,
		P: `_-_(
			4^#1:*expr.Constant_Int64Value#,
			-4.1^#2:*expr.Constant_DoubleValue#
		  )^#3:*expr.Expr_CallExpr#`,
	},
	{
		I: `b"abc"`,
		P: `b"abc"^#1:*expr.Constant_BytesValue#`,
	},
	{
		I: `23.39`,
		P: `23.39^#1:*expr.Constant_DoubleValue#`,
	},
	{
		I: `!a`,
		P: `!_(
    		  a^#1:*expr.Expr_IdentExpr#
			)^#2:*expr.Expr_CallExpr#`,
	},
	{
		I: `null`,
		P: `null^#1:*expr.Constant_NullValue#`,
	},
	{
		I: `a`,
		P: `a^#1:*expr.Expr_IdentExpr#`,
	},
	{
		I: `a?b:c`,
		P: `
			_?_:_(
    		  a^#1:*expr.Expr_IdentExpr#,
    		  b^#2:*expr.Expr_IdentExpr#,
    		  c^#3:*expr.Expr_IdentExpr#
			)^#4:*expr.Expr_CallExpr#`,
	},
	{
		I: `a || b`,
		P: `_||_(
    		  a^#1:*expr.Expr_IdentExpr#,
    		  b^#2:*expr.Expr_IdentExpr#
			)^#3:*expr.Expr_CallExpr#`,
	},
	{
		I: `a + b`,
		P: `_+_(
    		  a^#1:*expr.Expr_IdentExpr#,
    		  b^#2:*expr.Expr_IdentExpr#
			)^#3:*expr.Expr_CallExpr#`,
	},
	{
		I: `a - b`,
		P: `_-_(
    		  a^#1:*expr.Expr_IdentExpr#,
    		  b^#2:*expr.Expr_IdentExpr#
			)^#3:*expr.Expr_CallExpr#`,
	},
	{
		I: `a * b`,
		P: `_*_(
    		  a^#1:*expr.Expr_IdentExpr#,
    		  b^#2:*expr.Expr_IdentExpr#
			)^#3:*expr.Expr_CallExpr#`,
	},
	{
		I: `a / b`,
		P: `_/_(
    		  a^#1:*expr.Expr_IdentExpr#,
    		  b^#2:*expr.Expr_IdentExpr#
			)^#3:*expr.Expr_CallExpr#`,
	},
	{
		I: `a % b`,
		P: `_%_(
    		  a^#1:*expr.Expr_IdentExpr#,
    		  b^#2:*expr.Expr_IdentExpr#
			)^#3:*expr.Expr_CallExpr#`,
	},
	{
		I: `a in b`,
		P: `@in(
    		  a^#1:*expr.Expr_IdentExpr#,
    		  b^#2:*expr.Expr_IdentExpr#
			)^#3:*expr.Expr_CallExpr#`,
	},
	{
		I: `a == b`,
		P: `_==_(
    		  a^#1:*expr.Expr_IdentExpr#,
    		  b^#2:*expr.Expr_IdentExpr#
			)^#3:*expr.Expr_CallExpr#`,
	},
	{
		I: `a != b`,
		P: `_!=_(
    		  a^#1:*expr.Expr_IdentExpr#,
    		  b^#2:*expr.Expr_IdentExpr#
			)^#3:*expr.Expr_CallExpr#`,
	},
	{
		I: `a > b`,
		P: `_>_(
    		  a^#1:*expr.Expr_IdentExpr#,
    		  b^#2:*expr.Expr_IdentExpr#
			)^#3:*expr.Expr_CallExpr#`,
	},
	{
		I: `a >= b`,
		P: `_>=_(
    		  a^#1:*expr.Expr_IdentExpr#,
    		  b^#2:*expr.Expr_IdentExpr#
			)^#3:*expr.Expr_CallExpr#`,
	},
	{
		I: `a < b`,
		P: `_<_(
    		  a^#1:*expr.Expr_IdentExpr#,
    		  b^#2:*expr.Expr_IdentExpr#
			)^#3:*expr.Expr_CallExpr#`,
	},
	{
		I: `a <= b`,
		P: `_<=_(
    		  a^#1:*expr.Expr_IdentExpr#,
    		  b^#2:*expr.Expr_IdentExpr#
			)^#3:*expr.Expr_CallExpr#`,
	},
	{
		I: `a.b`,
		P: `a^#1:*expr.Expr_IdentExpr#.b^#2:*expr.Expr_SelectExpr#`,
	},
	{
		I: `a.b.c`,
		P: `a^#1:*expr.Expr_IdentExpr#.b^#2:*expr.Expr_SelectExpr#.c^#3:*expr.Expr_SelectExpr#`,
	},
	{
		I: `a[b]`,
		P: `_[_](
    		  a^#1:*expr.Expr_IdentExpr#,
    		  b^#2:*expr.Expr_IdentExpr#
			)^#3:*expr.Expr_CallExpr#`,
	},

	// TODO: This is an error.
	//{
	//  I: `foo{ a=b, c=d }`,
	//  P: `
	//
	//  `,
	//},

	{
		I: `foo{ }`,
		P: `foo{}^#2:*expr.Expr_StructExpr#`,
	},
	{
		I: `foo{ a:b }`,
		P: `foo{
    		  a:b^#2:*expr.Expr_IdentExpr#^#3:*expr.Expr_CreateStruct_Entry#
			}^#4:*expr.Expr_StructExpr#`,
	},
	{
		I: `foo{ a:b, c:d }`,
		P: `foo{
    		  a:b^#2:*expr.Expr_IdentExpr#^#3:*expr.Expr_CreateStruct_Entry#,
    		  c:d^#4:*expr.Expr_IdentExpr#^#5:*expr.Expr_CreateStruct_Entry#
			}^#6:*expr.Expr_StructExpr#`,
	},
	{
		I: `{}`,
		P: `{}^#1:*expr.Expr_StructExpr#`,
	},

	{
		I: `{a:b, c:d}`,
		P: `{
    		  a^#1:*expr.Expr_IdentExpr#:b^#2:*expr.Expr_IdentExpr#^#3:*expr.Expr_CreateStruct_Entry#,
    		  c^#4:*expr.Expr_IdentExpr#:d^#5:*expr.Expr_IdentExpr#^#6:*expr.Expr_CreateStruct_Entry#
			}^#7:*expr.Expr_StructExpr#`,
	},
	{
		I: `[]`,
		P: `[]^#1:*expr.Expr_ListExpr#`,
	},
	{
		I: `[a]`,
		P: `[
    		  a^#1:*expr.Expr_IdentExpr#
			]^#2:*expr.Expr_ListExpr#`,
	},
	{
		I: `[a, b, c]`,
		P: `[
    		  a^#1:*expr.Expr_IdentExpr#,
    		  b^#2:*expr.Expr_IdentExpr#,
    		  c^#3:*expr.Expr_IdentExpr#
			]^#4:*expr.Expr_ListExpr#`,
	},
	{
		I: `(a)`,
		P: `a^#1:*expr.Expr_IdentExpr#`,
	},
	{
		I: `((a))`,
		P: `a^#1:*expr.Expr_IdentExpr#`,
	},
	{
		I: `a()`,
		P: `a()^#1:*expr.Expr_CallExpr#`,
	},

	{
		I: `a(b)`,
		P: `a(
    		  b^#1:*expr.Expr_IdentExpr#)^#2:*expr.Expr_CallExpr#`,
	},

	{
		I: `a(b, c)`,
		P: `a(
    		  b^#1:*expr.Expr_IdentExpr#,
    		  c^#2:*expr.Expr_IdentExpr#)^#3:*expr.Expr_CallExpr#`,
	},
	{
		I: `a.b()`,
		P: `a^#1:*expr.Expr_IdentExpr#.b()^#2:*expr.Expr_CallExpr#`,
	},

	{
		I: `a.b(c)`,
		P: `a^#1:*expr.Expr_IdentExpr#.b(
    		  c^#2:*expr.Expr_IdentExpr#)^#3:*expr.Expr_CallExpr#`,
		L: `a^#1[1,0]#.b(
    		  c^#2[1,4]#
    		)^#3[1,3]#`,
	},

	// Parse error tests
	{
		I: `*@a | b`,
		E: `
ERROR: <input>:1:2: Syntax error: token recognition error at: '@'
 | *@a | b
 | .^
ERROR: <input>:1:1: Syntax error: extraneous input '*' expecting {'[', '{', '(', '.', '-', '!', 'true', 'false', 'null', NUM_FLOAT, NUM_INT, NUM_UINT, STRING, BYTES, IDENTIFIER}
 | *@a | b
 | ^
ERROR: <input>:1:5: Syntax error: token recognition error at: '| '
 | *@a | b
 | ....^
ERROR: <input>:1:7: Syntax error: extraneous input 'b' expecting <EOF>
 | *@a | b
 | ......^`,
	},
	{
		I: `a | b`,
		E: `
ERROR: <input>:1:3: Syntax error: token recognition error at: '| '
 | a | b
 | ..^
ERROR: <input>:1:5: Syntax error: extraneous input 'b' expecting <EOF>
 | a | b
 | ....^`,
	},

	// Macro tests
	{
		I: `has(m.f)`,
		P: `m^#1:*expr.Expr_IdentExpr#.f~test-only~^#3:*expr.Expr_SelectExpr#`,
		L: `m^#1[1,4]#.f~test-only~^#3[1,3]#`,
	},
	{
		I: `m.exists_one(v, f)`,
		P: `__comprehension__(
    		  // Variable
    		  v,
    		  // Target
    		  m^#1:*expr.Expr_IdentExpr#,
    		  // Accumulator
    		  __result__,
    		  // Init
    		  0^#4:*expr.Constant_Int64Value#,
    		  // LoopCondition
    		  _<=_(
    		    __result__^#6:*expr.Expr_IdentExpr#,
    		    1^#5:*expr.Constant_Int64Value#
  			  )^#7:*expr.Expr_CallExpr#,
    		  // LoopStep
    		  _?_:_(
    		    f^#3:*expr.Expr_IdentExpr#,
    		    _+_(
    		      __result__^#8:*expr.Expr_IdentExpr#,
    		      1^#5:*expr.Constant_Int64Value#
				)^#9:*expr.Expr_CallExpr#,
    		    __result__^#10:*expr.Expr_IdentExpr#
			  )^#11:*expr.Expr_CallExpr#,
    		  // Result
    		  _==_(
    		    __result__^#12:*expr.Expr_IdentExpr#,
    		    1^#5:*expr.Constant_Int64Value#
		      )^#13:*expr.Expr_CallExpr#)^#14:*expr.Expr_ComprehensionExpr#`,
	},
	{
		I: `m.map(v, f)`,
		P: `__comprehension__(
    		  // Variable
    		  v,
    		  // Target
    		  m^#1:*expr.Expr_IdentExpr#,
    		  // Accumulator
    		  __result__,
    		  // Init
    		  []^#5:*expr.Expr_ListExpr#,
    		  // LoopCondition
    		  true^#6:*expr.Constant_BoolValue#,
    		  // LoopStep
    		  _+_(
    		    __result__^#4:*expr.Expr_IdentExpr#,
    		    [
    		      f^#3:*expr.Expr_IdentExpr#
				]^#7:*expr.Expr_ListExpr#
 			  )^#8:*expr.Expr_CallExpr#,
    		  // Result
    		  __result__^#4:*expr.Expr_IdentExpr#)^#9:*expr.Expr_ComprehensionExpr#`,
	},

	{
		I: `m.map(v, p, f)`,
		P: `__comprehension__(
    		  // Variable
    		  v,
    		  // Target
    		  m^#1:*expr.Expr_IdentExpr#,
    		  // Accumulator
    		  __result__,
    		  // Init
    		  []^#6:*expr.Expr_ListExpr#,
    		  // LoopCondition
    		  true^#7:*expr.Constant_BoolValue#,
    		  // LoopStep
    		  _?_:_(
    		    p^#3:*expr.Expr_IdentExpr#,
    		    _+_(
    		      __result__^#5:*expr.Expr_IdentExpr#,
    		      [
    		        f^#4:*expr.Expr_IdentExpr#
				  ]^#8:*expr.Expr_ListExpr#
				)^#9:*expr.Expr_CallExpr#,
    		    __result__^#5:*expr.Expr_IdentExpr#
                )^#10:*expr.Expr_CallExpr#,
    		    // Result
    		    __result__^#5:*expr.Expr_IdentExpr#
				)^#11:*expr.Expr_ComprehensionExpr#`,
	},

	{
		I: `m.filter(v, p)`,
		P: `__comprehension__(
    		  // Variable
    		  v,
    		  // Target
    		  m^#1:*expr.Expr_IdentExpr#,
    		  // Accumulator
    		  __result__,
    		  // Init
    		  []^#5:*expr.Expr_ListExpr#,
    		  // LoopCondition
    		  true^#6:*expr.Constant_BoolValue#,
    		  // LoopStep
    		  _?_:_(
    		    p^#3:*expr.Expr_IdentExpr#,
    		    _+_(
    		      __result__^#4:*expr.Expr_IdentExpr#,
    		      [
    		        v^#2:*expr.Expr_IdentExpr#
				  ]^#7:*expr.Expr_ListExpr#
				)^#8:*expr.Expr_CallExpr#,
    		    __result__^#4:*expr.Expr_IdentExpr#
  				)^#9:*expr.Expr_CallExpr#,
    		    // Result
    		    __result__^#4:*expr.Expr_IdentExpr#)^#10:*expr.Expr_ComprehensionExpr#`,
	},

	// Tests from Java parser
	{
		I: `[] + [1,2,3,] + [4]`,
		P: `_+_(
    		  _+_(
    		    []^#1:*expr.Expr_ListExpr#,
    		    [
    		      1^#2:*expr.Constant_Int64Value#,
    		      2^#3:*expr.Constant_Int64Value#,
    		      3^#4:*expr.Constant_Int64Value#
    		    ]^#5:*expr.Expr_ListExpr#
    		  )^#6:*expr.Expr_CallExpr#,
    		  [
    		    4^#7:*expr.Constant_Int64Value#
    		  ]^#8:*expr.Expr_ListExpr#
    		)^#9:*expr.Expr_CallExpr#`,
	},
	{
		I: `{1:2u, 2:3u}`,
		P: `{
    		  1^#1:*expr.Constant_Int64Value#:2u^#2:*expr.Constant_Uint64Value#^#3:*expr.Expr_CreateStruct_Entry#,
    		  2^#4:*expr.Constant_Int64Value#:3u^#5:*expr.Constant_Uint64Value#^#6:*expr.Expr_CreateStruct_Entry#
    		}^#7:*expr.Expr_StructExpr#`,
	},
	{
		I: `TestAllTypes{single_int32: 1, single_int64: 2}`,
		P: `TestAllTypes{
    		  single_int32:1^#2:*expr.Constant_Int64Value#^#3:*expr.Expr_CreateStruct_Entry#,
    		  single_int64:2^#4:*expr.Constant_Int64Value#^#5:*expr.Expr_CreateStruct_Entry#
    		}^#6:*expr.Expr_StructExpr#`,
	},
	{
		I: `TestAllTypes(){single_int32: 1, single_int64: 2}`,
		E: `
ERROR: <input>:1:13: expected a qualified name
 | TestAllTypes(){single_int32: 1, single_int64: 2}
 | ............^
`,
	},
	{
		I: `size(x) == x.size()`,
		P: `_==_(
    		  size(
    		    x^#1:*expr.Expr_IdentExpr#
    		  )^#2:*expr.Expr_CallExpr#,
    		  x^#3:*expr.Expr_IdentExpr#.size()^#4:*expr.Expr_CallExpr#
    		)^#5:*expr.Expr_CallExpr#`,
	},
	{
		I: `1 + $`,
		E: `
ERROR: <input>:1:5: Syntax error: token recognition error at: '$'
 | 1 + $
 | ....^
ERROR: <input>:1:6: Syntax error: mismatched input '<EOF>' expecting {'[', '{', '(', '.', '-', '!', 'true', 'false', 'null', NUM_FLOAT, NUM_INT, NUM_UINT, STRING, BYTES, IDENTIFIER}
 | 1 + $
 | .....^
`,
	},
	{
		I: `1 + 2
3 +`,
		E: `
ERROR: <input>:2:1: Syntax error: mismatched input '3' expecting <EOF>
 | 3 +
 | ^
		`,
	},
	{
		I: `"\""`,
		P: `"\""^#1:*expr.Constant_StringValue#`,
	},
	{
		I: `[1,3,4][0]`,
		P: `_[_](
    		  [
    		    1^#1:*expr.Constant_Int64Value#,
    		    3^#2:*expr.Constant_Int64Value#,
    		    4^#3:*expr.Constant_Int64Value#
    		  ]^#4:*expr.Expr_ListExpr#,
    		  0^#5:*expr.Constant_Int64Value#
    		)^#6:*expr.Expr_CallExpr#`,
	},
	{
		I: `1.all(2, 3)`,
		E: `
ERROR: <input>:1:7: argument must be a simple name
 | 1.all(2, 3)
 | ......^
		`,
	},
	{
		I: `x["a"].single_int32 == 23`,
		P: `_==_(
    		  _[_](
    		    x^#1:*expr.Expr_IdentExpr#,
    		    "a"^#2:*expr.Constant_StringValue#
    		  )^#3:*expr.Expr_CallExpr#.single_int32^#4:*expr.Expr_SelectExpr#,
    		  23^#5:*expr.Constant_Int64Value#
    		)^#6:*expr.Expr_CallExpr#`,
	},
	{
		I: `x.single_nested_message != null`,
		P: `_!=_(
    		  x^#1:*expr.Expr_IdentExpr#.single_nested_message^#2:*expr.Expr_SelectExpr#,
    		  null^#3:*expr.Constant_NullValue#
    		)^#4:*expr.Expr_CallExpr#`,
	},
	{
		I: `false && !true || false ? 2 : 3`,
		P: `_?_:_(
    		  _||_(
    		    _&&_(
    		      false^#1:*expr.Constant_BoolValue#,
    		      !_(
    		        true^#2:*expr.Constant_BoolValue#
    		      )^#3:*expr.Expr_CallExpr#
    		    )^#4:*expr.Expr_CallExpr#,
    		    false^#5:*expr.Constant_BoolValue#
    		  )^#6:*expr.Expr_CallExpr#,
    		  2^#7:*expr.Constant_Int64Value#,
    		  3^#8:*expr.Constant_Int64Value#
    		)^#9:*expr.Expr_CallExpr#`,
	},
	{
		I: `b"abc" + B"def"`,
		P: `_+_(
    		  b"abc"^#1:*expr.Constant_BytesValue#,
    		  b"def"^#2:*expr.Constant_BytesValue#
    		)^#3:*expr.Expr_CallExpr#`,
	},
	{
		I: `1 + 2 * 3 - 1 / 2 == 6 % 1`,
		P: `_==_(
    		  _-_(
    		    _+_(
    		      1^#1:*expr.Constant_Int64Value#,
    		      _*_(
    		        2^#2:*expr.Constant_Int64Value#,
    		        3^#3:*expr.Constant_Int64Value#
    		      )^#4:*expr.Expr_CallExpr#
    		    )^#5:*expr.Expr_CallExpr#,
    		    _/_(
    		      1^#6:*expr.Constant_Int64Value#,
    		      2^#7:*expr.Constant_Int64Value#
    		    )^#8:*expr.Expr_CallExpr#
    		  )^#9:*expr.Expr_CallExpr#,
    		  _%_(
    		    6^#10:*expr.Constant_Int64Value#,
    		    1^#11:*expr.Constant_Int64Value#
    		  )^#12:*expr.Expr_CallExpr#
    		)^#13:*expr.Expr_CallExpr#`,
	},
	{
		I: `1 + +`,
		E: `
ERROR: <input>:1:5: Syntax error: mismatched input '+' expecting {'[', '{', '(', '.', '-', '!', 'true', 'false', 'null', NUM_FLOAT, NUM_INT, NUM_UINT, STRING, BYTES, IDENTIFIER}
 | 1 + +
 | ....^
ERROR: <input>:1:6: Syntax error: mismatched input '<EOF>' expecting {'[', '{', '(', '.', '-', '!', 'true', 'false', 'null', NUM_FLOAT, NUM_INT, NUM_UINT, STRING, BYTES, IDENTIFIER}
 | 1 + +
 | .....^
		`,
	},
	{
		I: `"abc" + "def"`,
		P: `_+_(
    		  "abc"^#1:*expr.Constant_StringValue#,
    		  "def"^#2:*expr.Constant_StringValue#
    		)^#3:*expr.Expr_CallExpr#`,
	},

	{
		I: `{"a": 1}."a"`,
		E: `ERROR: <input>:1:10: Syntax error: mismatched input '"a"' expecting IDENTIFIER
             | {"a": 1}."a"
    		 | .........^`,
	},

	{
		I: `"\xC3\XBF"`,
		P: `"Ã¿"^#1:*expr.Constant_StringValue#`,
	},

	{
		I: `"\303\277"`,
		P: `"Ã¿"^#1:*expr.Constant_StringValue#`,
	},

	{
		I: `"hi\u263A \u263Athere"`,
		P: `"hi☺ ☺there"^#1:*expr.Constant_StringValue#`,
	},

	{
		I: `"\U000003A8\?"`,
		P: `"Ψ?"^#1:*expr.Constant_StringValue#`,
	},

	{
		I: `"\a\b\f\n\r\t\v'\"\\\? Legal escapes"`,
		P: `"\a\b\f\n\r\t\v'\"\\? Legal escapes"^#1:*expr.Constant_StringValue#`,
	},

	{
		I: `"\xFh"`,
		E: `ERROR: <input>:1:1: Syntax error: token recognition error at: '"\xFh'
    		 | "\xFh"
    		 | ^
    		ERROR: <input>:1:6: Syntax error: token recognition error at: '"'
    		 | "\xFh"
    		 | .....^
    		ERROR: <input>:1:7: Syntax error: mismatched input '<EOF>' expecting {'[', '{', '(', '.', '-', '!', 'true', 'false', 'null', NUM_FLOAT, NUM_INT, NUM_UINT, STRING, BYTES, IDENTIFIER}
    		 | "\xFh"
    		 | ......^`,
	},

	{
		I: `"\a\b\f\n\r\t\v\'\"\\\? Illegal escape \>"`,
		E: `ERROR: <input>:1:1: Syntax error: token recognition error at: '"\a\b\f\n\r\t\v\'\"\\\? Illegal escape \>'
    		 | "\a\b\f\n\r\t\v\'\"\\\? Illegal escape \>"
    		 | ^
    		ERROR: <input>:1:42: Syntax error: token recognition error at: '"'
    		 | "\a\b\f\n\r\t\v\'\"\\\? Illegal escape \>"
    		 | .........................................^
    		ERROR: <input>:1:43: Syntax error: mismatched input '<EOF>' expecting {'[', '{', '(', '.', '-', '!', 'true', 'false', 'null', NUM_FLOAT, NUM_INT, NUM_UINT, STRING, BYTES, IDENTIFIER}
    		 | "\a\b\f\n\r\t\v\'\"\\\? Illegal escape \>"
    		 | ..........................................^`,
	},

	{
		I: `      '😁' in ['😁', '😑', '😦'] 
			&& in.😁`,
		E: `ERROR: <input>:2:7: Syntax error: extraneous input 'in' expecting {'[', '{', '(', '.', '-', '!', 'true', 'false', 'null', NUM_FLOAT, NUM_INT, NUM_UINT, STRING, BYTES, IDENTIFIER}
		|    && in.😁
		| ......^
	   ERROR: <input>:2:10: Syntax error: token recognition error at: '😁'
		|    && in.😁
		| .........＾
	   ERROR: <input>:2:11: Syntax error: missing IDENTIFIER at '<EOF>'
		|    && in.😁
		| .........．^`,
	},
}

type testInfo struct {
	// I contains the input expression to be parsed.
	I string

	// P contains the type/id adorned debug output of the expression tree.
	P string

	// E contains the expected error output for a failed parse, or "" if the parse is expected to be successful.
	E string

	// L contains the expected source adorned debug output of the expression tree.
	L string
}

type metadata interface {
	GetLocation(exprID int64) (common.Location, bool)
}

type kindAndIDAdorner struct {
}

func (k *kindAndIDAdorner) GetMetadata(elem interface{}) string {
	switch elem.(type) {
	case *exprpb.Expr:
		e := elem.(*exprpb.Expr)
		var valType interface{} = e.ExprKind
		switch valType.(type) {
		case *exprpb.Expr_ConstExpr:
			valType = e.GetConstExpr().ConstantKind
		}
		return fmt.Sprintf("^#%d:%s#", e.Id, reflect.TypeOf(valType))
	case *exprpb.Expr_CreateStruct_Entry:
		entry := elem.(*exprpb.Expr_CreateStruct_Entry)
		return fmt.Sprintf("^#%d:%s#", entry.Id, "*expr.Expr_CreateStruct_Entry")
	}
	return ""
}

type locationAdorner struct {
	sourceInfo *exprpb.SourceInfo
}

var _ metadata = &locationAdorner{}

func (l *locationAdorner) GetLocation(exprID int64) (common.Location, bool) {
	if pos, found := l.sourceInfo.Positions[exprID]; found {
		var line = 1
		for _, lineOffset := range l.sourceInfo.LineOffsets {
			if lineOffset > pos {
				break
			} else {
				line++
			}
		}
		var column = pos
		if line > 1 {
			column = pos - l.sourceInfo.LineOffsets[line-2]
		}
		return common.NewLocation(line, int(column)), true
	}
	return common.NoLocation, false
}

func (l *locationAdorner) GetMetadata(elem interface{}) string {
	var elemID int64
	switch elem.(type) {
	case *exprpb.Expr:
		elemID = elem.(*exprpb.Expr).Id
	case *exprpb.Expr_CreateStruct_Entry:
		elemID = elem.(*exprpb.Expr_CreateStruct_Entry).Id
	}
	location, _ := l.GetLocation(elemID)
	return fmt.Sprintf("^#%d[%d,%d]#", elemID, location.Line(), location.Column())
}

func TestParse(t *testing.T) {
	for i, tst := range testCases {
		name := fmt.Sprintf("%d %s", i, tst.I)
		t.Run(name, func(tt *testing.T) {

			src := common.NewTextSource(tst.I)
			expression, errors := Parse(src)
			if len(errors.GetErrors()) > 0 {
				actualErr := errors.ToDisplayString()
				if tst.E == "" {
					tt.Fatalf("Unexpected errors: %v", actualErr)
				} else if !test.Compare(actualErr, tst.E) {
					tt.Fatalf(test.DiffMessage("Error mismatch", actualErr, tst.E))
				}
				return
			} else if tst.E != "" {
				tt.Fatalf("Expected error not thrown: '%s'", tst.E)
			}

			actualWithKind := debug.ToAdornedDebugString(expression.Expr, &kindAndIDAdorner{})
			if !test.Compare(actualWithKind, tst.P) {
				tt.Fatal(test.DiffMessage("structure", actualWithKind, tst.P))
			}

			if tst.L != "" {
				actualWithLocation := debug.ToAdornedDebugString(expression.Expr, &locationAdorner{expression.SourceInfo})
				if !test.Compare(actualWithLocation, tst.L) {
					tt.Fatal(test.DiffMessage("location", actualWithLocation, tst.L))
				}
			}
		})
	}
}
