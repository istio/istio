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

package checker

import (
	"fmt"
	"testing"

	"github.com/golang/protobuf/proto"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/packages"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/parser"
	"github.com/google/cel-go/test"
	"github.com/google/cel-go/test/proto3pb"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

var testCases = []testInfo{
	// Const types
	{
		I:    `"A"`,
		R:    `"A"~string`,
		Type: decls.String,
	},
	{
		I:    `12`,
		R:    `12~int`,
		Type: decls.Int,
	},
	{
		I:    `12u`,
		R:    `12u~uint`,
		Type: decls.Uint,
	},
	{
		I:    `true`,
		R:    `true~bool`,
		Type: decls.Bool,
	},
	{
		I:    `false`,
		R:    `false~bool`,
		Type: decls.Bool,
	},
	{
		I:    `12.23`,
		R:    `12.23~double`,
		Type: decls.Double,
	},
	{
		I:    `null`,
		R:    `null~null`,
		Type: decls.Null,
	},
	{
		I:    `b"ABC"`,
		R:    `b"ABC"~bytes`,
		Type: decls.Bytes,
	},

	// Ident types
	{
		I:    `is`,
		R:    `is~string^is`,
		Type: decls.String,
		Env:  testEnvs["default"],
	},
	{
		I:    `ii`,
		R:    `ii~int^ii`,
		Type: decls.Int,
		Env:  testEnvs["default"],
	},
	{
		I:    `iu`,
		R:    `iu~uint^iu`,
		Type: decls.Uint,
		Env:  testEnvs["default"],
	},
	{
		I:    `iz`,
		R:    `iz~bool^iz`,
		Type: decls.Bool,
		Env:  testEnvs["default"],
	},
	{
		I:    `id`,
		R:    `id~double^id`,
		Type: decls.Double,
		Env:  testEnvs["default"],
	},
	{
		I:    `ix`,
		R:    `ix~null^ix`,
		Type: decls.Null,
		Env:  testEnvs["default"],
	},
	{
		I:    `ib`,
		R:    `ib~bytes^ib`,
		Type: decls.Bytes,
		Env:  testEnvs["default"],
	},
	{
		I:    `id`,
		R:    `id~double^id`,
		Type: decls.Double,
		Env:  testEnvs["default"],
	},

	{
		I:    `[]`,
		R:    `[]~list(dyn)`,
		Type: decls.NewListType(decls.Dyn),
	},
	{
		I:    `[1]`,
		R:    `[1~int]~list(int)`,
		Type: decls.NewListType(decls.Int),
	},

	{
		I:    `[1, "A"]`,
		R:    `[1~int, "A"~string]~list(dyn)`,
		Type: decls.NewListType(decls.Dyn),
	},
	{
		I:    `foo`,
		R:    `foo~!error!`,
		Type: decls.Error,
		Error: `
ERROR: <input>:1:1: undeclared reference to 'foo' (in container '')
| foo
| ^`,
	},

	// Call resolution
	{
		I:    `fg_s()`,
		R:    `fg_s()~string^fg_s_0`,
		Type: decls.String,
		Env:  testEnvs["default"],
	},
	{
		I:    `is.fi_s_s()`,
		R:    `is~string^is.fi_s_s()~string^fi_s_s_0`,
		Type: decls.String,
		Env:  testEnvs["default"],
	},
	{
		I:    `1 + 2`,
		R:    `_+_(1~int, 2~int)~int^add_int64`,
		Type: decls.Int,
		Env:  testEnvs["default"],
	},
	{
		I:    `1 + ii`,
		R:    `_+_(1~int, ii~int^ii)~int^add_int64`,
		Type: decls.Int,
		Env:  testEnvs["default"],
	},
	{
		I:    `[1] + [2]`,
		R:    `_+_([1~int]~list(int), [2~int]~list(int))~list(int)^add_list`,
		Type: decls.NewListType(decls.Int),
		Env:  testEnvs["default"],
	},

	// Tests from Java implementation
	{
		I:    `[] + [1,2,3,] + [4]`,
		Type: decls.NewListType(decls.Int),
		R: `
	_+_(
		_+_(
			[]~list(int),
			[1~int, 2~int, 3~int]~list(int))~list(int)^add_list,
			[4~int]~list(int))
	~list(int)^add_list
	`,
	},

	{
		I: `[1, 2u] + []`,
		R: `_+_(
			[
				1~int,
				2u~uint
			]~list(dyn),
			[]~list(dyn)
		)~list(dyn)^add_list`,
		Type: decls.NewListType(decls.Dyn),
	},

	{
		I:    `{1:2u, 2:3u}`,
		Type: decls.NewMapType(decls.Int, decls.Uint),
		R:    `{1~int : 2u~uint, 2~int : 3u~uint}~map(int, uint)`,
	},

	{
		I:    `{"a":1, "b":2}.a`,
		Type: decls.Int,
		R:    `{"a"~string : 1~int, "b"~string : 2~int}~map(string, int).a~int`,
	},
	{
		I:    `{1:2u, 2u:3}`,
		Type: decls.NewMapType(decls.Dyn, decls.Dyn),
		R:    `{1~int : 2u~uint, 2u~uint : 3~int}~map(dyn, dyn)`,
	},
	{
		I:         `TestAllTypes{single_int32: 1, single_int64: 2}`,
		Container: "google.expr.proto3.test",
		R: `
	TestAllTypes{single_int32 : 1~int, single_int64 : 2~int}
	  ~google.expr.proto3.test.TestAllTypes
	    ^google.expr.proto3.test.TestAllTypes`,
		Type: decls.NewObjectType("google.expr.proto3.test.TestAllTypes"),
	},

	{
		I:         `TestAllTypes{single_int32: 1u}`,
		Container: "google.expr.proto3.test",
		Error: `
	ERROR: <input>:1:26: expected type of field 'single_int32' is 'int' but provided type is 'uint'
	  | TestAllTypes{single_int32: 1u}
	  | .........................^`,
	},

	{
		I:         `TestAllTypes{single_int32: 1, undefined: 2}`,
		Container: "google.expr.proto3.test",
		Error: `
	ERROR: <input>:1:40: undefined field 'undefined'
	  | TestAllTypes{single_int32: 1, undefined: 2}
	  | .......................................^`,
	},

	{
		I: `size(x) == x.size()`,
		R: `
_==_(size(x~list(int)^x)~int^size_list, x~list(int)^x.size()~int^list_size)
  ~bool^equals`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewListType(decls.Int), nil),
			},
		},
		Type: decls.Bool,
	},
	{
		I: `int(1u) + int(uint("1"))`,
		R: `
_+_(int(1u~uint)~int^uint64_to_int64,
      int(uint("1"~string)~uint^string_to_uint64)~int^uint64_to_int64)
  ~int^add_int64`,
		Type: decls.Int,
	},

	{
		I: `false && !true || false ? 2 : 3`,
		R: `
_?_:_(_||_(_&&_(false~bool, !_(true~bool)~bool^logical_not)~bool^logical_and,
            false~bool)
        ~bool^logical_or,
      2~int,
      3~int)
  ~int^conditional
`,
		Type: decls.Int,
	},

	{
		I:    `b"abc" + b"def"`,
		R:    `_+_(b"abc"~bytes, b"def"~bytes)~bytes^add_bytes`,
		Type: decls.Bytes,
	},

	{
		I: `1.0 + 2.0 * 3.0 - 1.0 / 2.20202 != 66.6`,
		R: `
_!=_(_-_(_+_(1~double, _*_(2~double, 3~double)~double^multiply_double)
           ~double^add_double,
           _/_(1~double, 2.20202~double)~double^divide_double)
       ~double^subtract_double,
      66.6~double)
  ~bool^not_equals`,
		Type: decls.Bool,
	},

	{
		I:    `1 + 2 * 3 - 1 / 2 == 6 % 1`,
		R:    ` _==_(_-_(_+_(1~int, _*_(2~int, 3~int)~int^multiply_int64)~int^add_int64, _/_(1~int, 2~int)~int^divide_int64)~int^subtract_int64, _%_(6~int, 1~int)~int^modulo_int64)~bool^equals`,
		Type: decls.Bool,
	},

	{
		I:    `"abc" + "def"`,
		R:    `_+_("abc"~string, "def"~string)~string^add_string`,
		Type: decls.String,
	},

	{
		I: `1u + 2u * 3u - 1u / 2u == 6u % 1u`,
		R: `_==_(_-_(_+_(1u~uint, _*_(2u~uint, 3u~uint)~uint^multiply_uint64)
	         ~uint^add_uint64,
	         _/_(1u~uint, 2u~uint)~uint^divide_uint64)
	     ~uint^subtract_uint64,
	    _%_(6u~uint, 1u~uint)~uint^modulo_uint64)
	~bool^equals`,
		Type: decls.Bool,
	},

	{
		I: `x.single_int32 != null`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewObjectType("google.expr.proto3.test.Proto2Message"), nil),
			},
		},
		Error: `
	ERROR: <input>:1:2: [internal] unexpected failed resolution of 'google.expr.proto3.test.Proto2Message'
	  | x.single_int32 != null
	  | .^
	`,
	},

	{
		I: `x.single_value + 1 / x.single_struct.y == 23`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewObjectType("google.expr.proto3.test.TestAllTypes"), nil),
			},
		},
		R: `_==_(
			_+_(
			  x~google.expr.proto3.test.TestAllTypes^x.single_value~dyn,
			  _/_(
				1~int,
				x~google.expr.proto3.test.TestAllTypes^x.single_struct~map(string, dyn).y~dyn
			  )~int^divide_int64
			)~int^add_int64,
			23~int
		  )~bool^equals`,
		Type: decls.Bool,
	},

	{
		I: `x.single_value[23] + x.single_struct['y']`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewObjectType("google.expr.proto3.test.TestAllTypes"), nil),
			},
		},
		R: `_+_(
			_[_](
			  x~google.expr.proto3.test.TestAllTypes^x.single_value~dyn,
			  23~int
			)~dyn^index_list|index_map,
			_[_](
			  x~google.expr.proto3.test.TestAllTypes^x.single_struct~map(string, dyn),
			  "y"~string
			)~dyn^index_map
		  )~dyn^add_int64|add_uint64|add_double|add_string|add_bytes|add_list|add_timestamp_duration|add_duration_timestamp|add_duration_duration
		  `,
		Type: decls.Dyn,
	},

	{
		I:         `TestAllTypes.NestedEnum.BAR != 99`,
		Container: "google.expr.proto3.test",
		R: `_!=_(TestAllTypes.NestedEnum.BAR
	     ~int^google.expr.proto3.test.TestAllTypes.NestedEnum.BAR,
	    99~int)
	~bool^not_equals`,
		Type: decls.Bool,
	},

	{
		I:    `size([] + [1])`,
		R:    `size(_+_([]~list(int), [1~int]~list(int))~list(int)^add_list)~int^size_list`,
		Type: decls.Int,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewObjectType("google.expr.proto3.test.TestAllTypes"), nil),
			},
		},
	},

	{
		I: `x["claims"]["groups"][0].name == "dummy"
		&& x.claims["exp"] == y[1].time
		&& x.claims.structured == {'key': z}
		&& z == 1.0`,
		R: `_&&_(
			_&&_(
			  _&&_(
				_==_(
				  _[_](
					_[_](
					  _[_](
						x~map(string, dyn)^x,
						"claims"~string
					  )~dyn^index_map,
					  "groups"~string
					)~list(string)^index_map,
					0~int
				  )~string^index_list.name~string,
				  "dummy"~string
				)~bool^equals,
				_==_(
				  _[_](
					x~map(string, dyn)^x.claims~dyn,
					"exp"~string
				  )~dyn^index_map,
				  _[_](
					y~list(dyn)^y,
					1~int
				  )~dyn^index_list.time~dyn
				)~bool^equals
			  )~bool^logical_and,
			  _==_(
				x~map(string, dyn)^x.claims~dyn.structured~dyn,
				{
				  "key"~string:z~dyn^z
				}~map(string, dyn)
			  )~bool^equals
			)~bool^logical_and,
			_==_(
			  z~dyn^z,
			  1~double
			)~bool^equals
		  )~bool^logical_and`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewObjectType("google.protobuf.Struct"), nil),
				decls.NewIdent("y", decls.NewObjectType("google.protobuf.ListValue"), nil),
				decls.NewIdent("z", decls.NewObjectType("google.protobuf.Value"), nil),
			},
		},
		Type: decls.Bool,
	},

	{
		I: `x + y`,
		R: ``,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewListType(decls.NewObjectType("google.expr.proto3.test.TestAllTypes")), nil),
				decls.NewIdent("y", decls.NewListType(decls.Int), nil),
			},
		},
		Error: `
ERROR: <input>:1:3: found no matching overload for '_+_' applied to '(list(google.expr.proto3.test.TestAllTypes), list(int))'
  | x + y
  | ..^
		`,
	},

	{
		I: `x[1u]`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewListType(decls.NewObjectType("google.expr.proto3.test.TestAllTypes")), nil),
			},
		},
		Error: `
ERROR: <input>:1:2: found no matching overload for '_[_]' applied to '(list(google.expr.proto3.test.TestAllTypes), uint)'
  | x[1u]
  | .^
`,
	},

	{
		I: `(x + x)[1].single_int32 == size(x)`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewListType(decls.NewObjectType("google.expr.proto3.test.TestAllTypes")), nil),
			},
		},
		R: `
_==_(_[_](_+_(x~list(google.expr.proto3.test.TestAllTypes)^x,
                x~list(google.expr.proto3.test.TestAllTypes)^x)
            ~list(google.expr.proto3.test.TestAllTypes)^add_list,
           1~int)
       ~google.expr.proto3.test.TestAllTypes^index_list
       .
       single_int32
       ~int,
      size(x~list(google.expr.proto3.test.TestAllTypes)^x)~int^size_list)
  ~bool^equals
	`,
		Type: decls.Bool,
	},

	{
		I: `x.repeated_int64[x.single_int32] == 23`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewObjectType("google.expr.proto3.test.TestAllTypes"), nil),
			},
		},
		R: `
_==_(_[_](x~google.expr.proto3.test.TestAllTypes^x.repeated_int64~list(int),
           x~google.expr.proto3.test.TestAllTypes^x.single_int32~int)
       ~int^index_list,
      23~int)
  ~bool^equals`,
		Type: decls.Bool,
	},

	{
		I: `size(x.map_int64_nested_type) == 0`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewObjectType("google.expr.proto3.test.TestAllTypes"), nil),
			},
		},
		R: `
_==_(size(x~google.expr.proto3.test.TestAllTypes^x.map_int64_nested_type
            ~map(int, google.expr.proto3.test.NestedTestAllTypes))
       ~int^size_map,
      0~int)
  ~bool^equals
		`,
		Type: decls.Bool,
	},

	{
		I: `x.repeated_int64.map(x, double(x))`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewObjectType("google.expr.proto3.test.TestAllTypes"), nil),
			},
		},
		R: `
		__comprehension__(
    		  // Variable
    		  x,
    		  // Target
    		  x~google.expr.proto3.test.TestAllTypes^x.repeated_int64~list(int),
    		  // Accumulator
    		  __result__,
    		  // Init
    		  []~list(double),
    		  // LoopCondition
    		  true~bool,
    		  // LoopStep
    		  _+_(
    		    __result__~list(double)^__result__,
    		    [
    		      double(
    		        x~int^x
    		      )~double^int64_to_double
    		    ]~list(double)
    		  )~list(double)^add_list,
    		  // Result
    		  __result__~list(double)^__result__)~list(double)
		`,
		Type: decls.NewListType(decls.Double),
	},

	{
		I: `x.repeated_int64.map(x, x > 0, double(x))`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewObjectType("google.expr.proto3.test.TestAllTypes"), nil),
			},
		},
		R: `
	__comprehension__(
    		  // Variable
    		  x,
    		  // Target
    		  x~google.expr.proto3.test.TestAllTypes^x.repeated_int64~list(int),
    		  // Accumulator
    		  __result__,
    		  // Init
    		  []~list(double),
    		  // LoopCondition
    		  true~bool,
    		  // LoopStep
    		  _?_:_(
    		    _>_(
    		      x~int^x,
    		      0~int
    		    )~bool^greater_int64,
    		    _+_(
    		      __result__~list(double)^__result__,
    		      [
    		        double(
    		          x~int^x
    		        )~double^int64_to_double
    		      ]~list(double)
    		    )~list(double)^add_list,
    		    __result__~list(double)^__result__
    		  )~list(double)^conditional,
    		  // Result
    		  __result__~list(double)^__result__)~list(double)
		`,
		Type: decls.NewListType(decls.Double),
	},

	{
		I: `x[2].single_int32 == 23`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x",
					decls.NewMapType(decls.String,
						decls.NewObjectType("google.expr.proto3.test.TestAllTypes")), nil),
			},
		},
		Error: `
ERROR: <input>:1:2: found no matching overload for '_[_]' applied to '(map(string, google.expr.proto3.test.TestAllTypes), int)'
  | x[2].single_int32 == 23
  | .^
		`,
	},

	{
		I: `x["a"].single_int32 == 23`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x",
					decls.NewMapType(decls.String,
						decls.NewObjectType("google.expr.proto3.test.TestAllTypes")), nil),
			},
		},
		R: `
		_==_(_[_](x~map(string, google.expr.proto3.test.TestAllTypes)^x, "a"~string)
		~google.expr.proto3.test.TestAllTypes^index_map
		.
		single_int32
		~int,
		23~int)
		~bool^equals`,
		Type: decls.Bool,
	},

	{
		I: `x.single_nested_message.bb == 43 && has(x.single_nested_message)`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewObjectType("google.expr.proto3.test.TestAllTypes"), nil),
			},
		},

		// Our implementation code is expanding the macro
		R: `_&&_(
    		  _==_(
    		    x~google.expr.proto3.test.TestAllTypes^x.single_nested_message~google.expr.proto3.test.TestAllTypes.NestedMessage.bb~int,
    		    43~int
    		  )~bool^equals,
    		  x~google.expr.proto3.test.TestAllTypes^x.single_nested_message~test-only~~bool
    		)~bool^logical_and`,
		Type: decls.Bool,
	},

	{
		I: `x.single_nested_message.undefined == x.undefined && has(x.single_int32) && has(x.repeated_int32)`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewObjectType("google.expr.proto3.test.TestAllTypes"), nil),
			},
		},
		Error: `
ERROR: <input>:1:24: undefined field 'undefined'
 | x.single_nested_message.undefined == x.undefined && has(x.single_int32) && has(x.repeated_int32)
 | .......................^
ERROR: <input>:1:39: undefined field 'undefined'
 | x.single_nested_message.undefined == x.undefined && has(x.single_int32) && has(x.repeated_int32)
 | ......................................^
ERROR: <input>:1:56: field 'single_int32' does not support presence check
 | x.single_nested_message.undefined == x.undefined && has(x.single_int32) && has(x.repeated_int32)
 | .......................................................^
ERROR: <input>:1:79: field 'repeated_int32' does not support presence check
 | x.single_nested_message.undefined == x.undefined && has(x.single_int32) && has(x.repeated_int32)
 | ..............................................................................^
		`,
	},

	{
		I: `x.single_nested_message != null`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewObjectType("google.expr.proto3.test.TestAllTypes"), nil),
			},
		},
		R: `
		_!=_(x~google.expr.proto3.test.TestAllTypes^x.single_nested_message
		~google.expr.proto3.test.TestAllTypes.NestedMessage,
		null~null)
		~bool^not_equals
		`,
		Type: decls.Bool,
	},

	{
		I: `x.single_int64 != null`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewObjectType("google.expr.proto3.test.TestAllTypes"), nil),
			},
		},
		Error: `
ERROR: <input>:1:16: found no matching overload for '_!=_' applied to '(int, null)'
 | x.single_int64 != null
 | ...............^
		`,
	},

	{
		I: `x.single_int64_wrapper == null`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewObjectType("google.expr.proto3.test.TestAllTypes"), nil),
			},
		},
		R: `
		_==_(x~google.expr.proto3.test.TestAllTypes^x.single_int64_wrapper
		~wrapper(int),
		null~null)
		~bool^equals
		`,
		Type: decls.Bool,
	},
	{
		I: `x.repeated_int64.all(e, e > 0) && x.repeated_int64.exists(e, e < 0) && x.repeated_int64.exists_one(e, e == 0)`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewObjectType("google.expr.proto3.test.TestAllTypes"), nil),
			},
		},
		R: `_&&_(
			_&&_(
			  __comprehension__(
				// Variable
				e,
				// Target
				x~google.expr.proto3.test.TestAllTypes^x.repeated_int64~list(int),
				// Accumulator
				__result__,
				// Init
				true~bool,
				// LoopCondition
				@not_strictly_false(
				  __result__~bool^__result__
				)~bool^not_strictly_false,
				// LoopStep
				_&&_(
				  __result__~bool^__result__,
				  _>_(
					e~int^e,
					0~int
				  )~bool^greater_int64
				)~bool^logical_and,
				// Result
				__result__~bool^__result__)~bool,
			  __comprehension__(
				// Variable
				e,
				// Target
				x~google.expr.proto3.test.TestAllTypes^x.repeated_int64~list(int),
				// Accumulator
				__result__,
				// Init
				false~bool,
				// LoopCondition
				@not_strictly_false(
				  !_(
					__result__~bool^__result__
				  )~bool^logical_not
				)~bool^not_strictly_false,
				// LoopStep
				_||_(
				  __result__~bool^__result__,
				  _<_(
					e~int^e,
					0~int
				  )~bool^less_int64
				)~bool^logical_or,
				// Result
				__result__~bool^__result__)~bool
			)~bool^logical_and,
			__comprehension__(
			  // Variable
			  e,
			  // Target
			  x~google.expr.proto3.test.TestAllTypes^x.repeated_int64~list(int),
			  // Accumulator
			  __result__,
			  // Init
			  0~int,
			  // LoopCondition
			  _<=_(
				__result__~int^__result__,
				1~int
			  )~bool^less_equals_int64,
			  // LoopStep
			  _?_:_(
				_==_(
				  e~int^e,
				  0~int
				)~bool^equals,
				_+_(
				  __result__~int^__result__,
				  1~int
				)~int^add_int64,
				__result__~int^__result__
			  )~int^conditional,
			  // Result
			  _==_(
				__result__~int^__result__,
				1~int
			  )~bool^equals)~bool
		  )~bool^logical_and`,
		Type: decls.Bool,
	},

	{
		I: `x.all(e, 0)`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewObjectType("google.expr.proto3.test.TestAllTypes"), nil),
			},
		},
		Error: `
ERROR: <input>:1:1: expression of type 'google.expr.proto3.test.TestAllTypes' cannot be range of a comprehension (must be list, map, or dynamic)
 | x.all(e, 0)
 | ^
ERROR: <input>:1:6: found no matching overload for '_&&_' applied to '(bool, int)'
 | x.all(e, 0)
 | .....^
		`,
	},

	{
		I: `.google.expr.proto3.test.TestAllTypes`,
		R: `	.google.expr.proto3.test.TestAllTypes
	~type(google.expr.proto3.test.TestAllTypes)
	^google.expr.proto3.test.TestAllTypes`,
		Type: decls.NewTypeType(
			decls.NewObjectType("google.expr.proto3.test.TestAllTypes")),
	},

	{
		I:         `test.TestAllTypes`,
		Container: "google.expr.proto3",
		R: `
	test.TestAllTypes
	~type(google.expr.proto3.test.TestAllTypes)
	^google.expr.proto3.test.TestAllTypes
		`,
		Type: decls.NewTypeType(
			decls.NewObjectType("google.expr.proto3.test.TestAllTypes")),
	},

	{
		I: `1 + x`,
		Error: `
ERROR: <input>:1:5: undeclared reference to 'x' (in container '')
 | 1 + x
 | ....^`,
	},

	{
		I: `x == google.protobuf.Any{
				type_url:'types.googleapis.com/google.expr.proto3.test.TestAllTypes'
			} && x.single_nested_message.bb == 43
			|| x == google.expr.proto3.test.TestAllTypes{}
			|| y < x
			|| x >= x`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.Any, nil),
				decls.NewIdent("y", decls.NewWrapperType(decls.Int), nil),
			},
		},
		R: `
		_||_(
			_||_(
			  _||_(
				_&&_(
				  _==_(
					x~any^x,
					google.protobuf.Any{
					  type_url:"types.googleapis.com/google.expr.proto3.test.TestAllTypes"~string
					}~google.protobuf.Any^google.protobuf.Any
				  )~bool^equals,
				  _==_(
					x~any^x.single_nested_message~dyn.bb~dyn,
					43~int
				  )~bool^equals
				)~bool^logical_and,
				_==_(
				  x~any^x,
				  google.expr.proto3.test.TestAllTypes{}~google.expr.proto3.test.TestAllTypes^google.expr.proto3.test.TestAllTypes
				)~bool^equals
			  )~bool^logical_or,
			  _<_(
				y~wrapper(int)^y,
				x~any^x
			  )~bool^less_int64
			)~bool^logical_or,
			_>=_(
			  x~any^x,
			  x~any^x
			)~dyn^greater_equals_bool|greater_equals_int64|greater_equals_uint64|greater_equals_double|greater_equals_string|greater_equals_bytes|greater_equals_timestamp|greater_equals_duration
		  )~bool^logical_or
		`,
		Type: decls.Bool,
	},

	{
		I:         `x`,
		Container: "container",
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("container.x", decls.NewObjectType("google.expr.proto3.test.TestAllTypes"), nil),
			},
		},
		R:    `x~google.expr.proto3.test.TestAllTypes^container.x`,
		Type: decls.NewObjectType("google.expr.proto3.test.TestAllTypes"),
	},

	{
		I: `list == type([1]) && map == type({1:2u})`,
		R: `
_&&_(_==_(list~type(list(dyn))^list,
           type([1~int]~list(int))~type(list(int))^type)
       ~bool^equals,
      _==_(map~type(map(dyn, dyn))^map,
            type({1~int : 2u~uint}~map(int, uint))~type(map(int, uint))^type)
        ~bool^equals)
  ~bool^logical_and
	`,
		Type: decls.Bool,
	},

	{
		I: `myfun(1, true, 3u) + 1.myfun(false, 3u).myfun(true, 42u)`,
		Env: env{
			functions: []*exprpb.Decl{
				decls.NewFunction("myfun",
					decls.NewInstanceOverload("myfun_instance",
						[]*exprpb.Type{decls.Int, decls.Bool, decls.Uint}, decls.Int),
					decls.NewOverload("myfun_static",
						[]*exprpb.Type{decls.Int, decls.Bool, decls.Uint}, decls.Int)),
			},
		},
		R: `_+_(
    		  myfun(
    		    1~int,
    		    true~bool,
    		    3u~uint
    		  )~int^myfun_static,
    		  1~int.myfun(
    		    false~bool,
    		    3u~uint
    		  )~int^myfun_instance.myfun(
    		    true~bool,
    		    42u~uint
    		  )~int^myfun_instance
    		)~int^add_int64`,
		Type: decls.Int,
	},

	{
		I: `size(x) > 4`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewObjectType("google.expr.proto3.test.TestAllTypes"), nil),
			},
			functions: []*exprpb.Decl{
				decls.NewFunction("size",
					decls.NewOverload("size_message",
						[]*exprpb.Type{decls.NewObjectType("google.expr.proto3.test.TestAllTypes")},
						decls.Int)),
			},
		},
		Type: decls.Bool,
	},

	{
		I: `x.single_int64_wrapper + 1 != 23`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewObjectType("google.expr.proto3.test.TestAllTypes"), nil),
			},
		},
		R: `
		_!=_(_+_(x~google.expr.proto3.test.TestAllTypes^x.single_int64_wrapper
		~wrapper(int),
		1~int)
		~int^add_int64,
		23~int)
		~bool^not_equals
		`,
		Type: decls.Bool,
	},

	{
		I: `x.single_int64_wrapper + y != 23`,
		Env: env{
			idents: []*exprpb.Decl{
				decls.NewIdent("x", decls.NewObjectType("google.expr.proto3.test.TestAllTypes"), nil),
				decls.NewIdent("y", decls.NewObjectType("google.protobuf.Int32Value"), nil),
			},
		},
		R: `
		_!=_(
			_+_(
			  x~google.expr.proto3.test.TestAllTypes^x.single_int64_wrapper~wrapper(int),
			  y~wrapper(int)^y
			)~int^add_int64,
			23~int
		  )~bool^not_equals
		`,
		Type: decls.Bool,
	},

	{
		I: `1 in [1, 2, 3]`,
		R: `@in(
    		  1~int,
    		  [
    		    1~int,
    		    2~int,
    		    3~int
    		  ]~list(int)
    		)~bool^in_list`,
		Type: decls.Bool,
	},

	{
		I: `type(null) == null_type`,
		R: `_==_(
    		  type(
    		    null~null
    		  )~type(null)^type,
    		  null_type~type(null)^null_type
    		)~bool^equals`,
		Type: decls.Bool,
	},

	{
		I: `type(type) == type`,
		R: `_==_(
    		  type(
    		    type~type(type)^type
    		  )~type(type(type))^type,
    		  type~type(type)^type
    		)~bool^equals`,
		Type: decls.Bool,
	},
}

var typeProvider = initTypeProvider()

func initTypeProvider() ref.TypeProvider {
	return types.NewProvider(&proto3pb.NestedTestAllTypes{}, &proto3pb.TestAllTypes{})
}

var testEnvs = map[string]env{
	"default": {
		functions: []*exprpb.Decl{
			decls.NewFunction("fg_s",
				decls.NewOverload("fg_s_0", []*exprpb.Type{}, decls.String)),
			decls.NewFunction("fi_s_s",
				decls.NewInstanceOverload("fi_s_s_0",
					[]*exprpb.Type{decls.String}, decls.String)),
		},
		idents: []*exprpb.Decl{
			decls.NewIdent("is", decls.String, nil),
			decls.NewIdent("ii", decls.Int, nil),
			decls.NewIdent("iu", decls.Uint, nil),
			decls.NewIdent("iz", decls.Bool, nil),
			decls.NewIdent("ib", decls.Bytes, nil),
			decls.NewIdent("id", decls.Double, nil),
			decls.NewIdent("ix", decls.Null, nil),
		},
	},
}

type testInfo struct {
	// I contains the input expression to be parsed.
	I string

	// R contains the result output.
	R string

	// Type is the expected type of the expression
	Type *exprpb.Type

	// Container is the container name to use for test.
	Container string

	// Env is the environment to use for testing.
	Env env

	// Error is the expected error for negative test cases.
	Error string
}

type env struct {
	idents    []*exprpb.Decl
	functions []*exprpb.Decl
}

func TestCheck(t *testing.T) {
	for i, tst := range testCases {
		name := fmt.Sprintf("%d %s", i, tst.I)
		t.Run(name, func(tt *testing.T) {

			src := common.NewTextSource(tst.I)
			expression, errors := parser.Parse(src)
			if len(errors.GetErrors()) > 0 {
				tt.Fatalf("Unexpected parse errors: %v",
					errors.ToDisplayString())
				return
			}

			pkg := packages.NewPackage(tst.Container)
			env := NewStandardEnv(pkg, typeProvider)
			if tst.Env.idents != nil {
				for _, ident := range tst.Env.idents {
					env.Add(ident)
				}
			}
			if tst.Env.functions != nil {
				for _, fn := range tst.Env.functions {
					env.Add(fn)
				}
			}

			semantics, errors := Check(expression, src, env)
			if len(errors.GetErrors()) > 0 {
				errorString := errors.ToDisplayString()
				if tst.Error != "" {
					if !test.Compare(errorString, tst.Error) {
						tt.Error(test.DiffMessage("Error mismatch", errorString, tst.Error))
					}
				} else {
					tt.Errorf("Unexpected type-check errors: %v", errorString)
				}
			} else if tst.Error != "" {
				tt.Errorf("Expected error not thrown: %s", tst.Error)
			}

			actual := semantics.TypeMap[expression.Expr.Id]
			if tst.Error == "" {
				if actual == nil || !proto.Equal(actual, tst.Type) {
					tt.Error(test.DiffMessage("Type Error", actual, tst.Type))
				}
			}

			if tst.R != "" {
				actualStr := Print(expression.Expr, semantics)
				if !test.Compare(actualStr, tst.R) {
					tt.Error(test.DiffMessage("Structure error", actualStr, tst.R))
				}
			}
		})
	}
}
