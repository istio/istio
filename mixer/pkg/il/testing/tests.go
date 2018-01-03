// Copyright 2017 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ilt

import (
	"fmt"
	"net"
	"strings"
	"time"

	descriptor "istio.io/api/mixer/v1/config/descriptor"
	pb "istio.io/istio/mixer/pkg/config/proto"
	"istio.io/istio/mixer/pkg/expr"
)

var duration19, _ = time.ParseDuration("19ms")
var duration20, _ = time.ParseDuration("20ms")
var time1999 = time.Date(1999, time.December, 31, 23, 59, 0, 0, time.UTC)
var time1977 = time.Date(1977, time.February, 4, 12, 00, 0, 0, time.UTC)
var t, _ = time.Parse(time.RFC3339, "2015-01-02T15:04:35Z")
var t2, _ = time.Parse(time.RFC3339, "2015-01-02T15:04:34Z")

// TestData contains the common set of tests that is used by various components of il.
var TestData = []TestInfo{

	// Benchmark test cases
	{
		name:  `ExprBench/ok_1st`,
		Bench: true,
		E:     `ai == 20 || ar["foo"] == "bar"`,
		Type:  descriptor.BOOL,
		I: map[string]interface{}{
			"ai": int64(20),
			"ar": map[string]string{
				"foo": "bar",
			},
		},
		R: true,
		IL: `
 fn eval() bool
  resolve_i "ai"
  aeq_i 20
  jz L0
  apush_b true
  ret
L0:
  resolve_f "ar"
  anlookup "foo"
  aeq_s "bar"
  ret
end`,
	},
	{
		name:  `ExprBench/ok_2nd`,
		Bench: true,
		E:     `ai == 20 || ar["foo"] == "bar"`,
		Type:  descriptor.BOOL,
		I: map[string]interface{}{
			"ai": int64(2),
			"ar": map[string]string{
				"foo": "bar",
			},
		},
		R: true,
		IL: `
 fn eval() bool
  resolve_i "ai"
  aeq_i 20
  jz L0
  apush_b true
  ret
L0:
  resolve_f "ar"
  anlookup "foo"
  aeq_s "bar"
  ret
end`,
	},
	{
		name:  `ExprBench/not_found`,
		Bench: true,
		E:     `ai == 20 || ar["foo"] == "bar"`,
		Type:  descriptor.BOOL,
		I: map[string]interface{}{
			"ai": int64(2),
			"ar": map[string]string{
				"foo": "baz",
			},
		},
		R: false,
		IL: `
 fn eval() bool
  resolve_i "ai"
  aeq_i 20
  jz L0
  apush_b true
  ret
L0:
  resolve_f "ar"
  anlookup "foo"
  aeq_s "bar"
  ret
end`,
	},

	// Tests from expr/eval_test.go TestGoodEval
	{
		E:    `a == 2`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:    true,
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `a != 2`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:    false,
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `a != 2`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"d": int64(2),
		},
		Err:    "lookup failed: 'a'",
		AstErr: "unresolved attribute",
		conf:   TestConfigs["Expr/Eval"],
	},
	{
		E:    "2 != a",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"d": int64(2),
		},
		Err:    "lookup failed: 'a'",
		AstErr: "unresolved attribute",
		conf:   TestConfigs["Expr/Eval"],
	},
	{
		E:    "a ",
		Type: descriptor.INT64,
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:    int64(2),
		conf: TestConfigs["Expr/Eval"],
	},

	// Compilation Error due to type mismatch
	{
		E: "true == a",
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:          false,
		CompileErr: "EQ(true, $a) arg 2 ($a) typeError got INT64, expected BOOL",
		conf:       TestConfigs["Expr/Eval"],
	},

	// Compilation Error due to type mismatch
	{
		E: "3.14 == a",
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:          false,
		CompileErr: "EQ(3.14, $a) arg 2 ($a) typeError got INT64, expected DOUBLE",
		conf:       TestConfigs["Expr/Eval"],
	},

	{
		E:    "2 ",
		Type: descriptor.INT64,
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:    int64(2),
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `request.user == "user1"`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"request.user": "user1",
		},
		R:    true,
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `request.user2| request.user | "user1"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"request.user": "user2",
		},
		R:    "user2",
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `request.user2| request.user3 | "user1"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"request.user": "user2",
		},
		R:    "user1",
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `request.size| 200`,
		Type: descriptor.INT64,
		I: map[string]interface{}{
			"request.size": int64(120),
		},
		R:    int64(120),
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `request.size| 200`,
		Type: descriptor.INT64,
		I: map[string]interface{}{
			"request.size": int64(0),
		},
		R:    int64(0),
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `request.size| 200`,
		Type: descriptor.INT64,
		I: map[string]interface{}{
			"request.size1": int64(0),
		},
		R:    int64(200),
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `(x == 20 && y == 10) || x == 30`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"x": int64(20),
			"y": int64(10),
		},
		R:    true,
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `x == 20 && y == 10`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"a": int64(20),
			"b": int64(10),
		},
		Err:    "lookup failed: 'x'",
		AstErr: "unresolved attribute",
		conf:   TestConfigs["Expr/Eval"],
	},
	{
		E:    `match(service.name, "*.ns1.cluster") && service.user == "admin"`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"service.name": "svc1.ns1.cluster",
			"service.user": "admin",
		},
		R:    true,
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `( origin.name | "unknown" ) == "users"`,
		Type: descriptor.BOOL,
		I:    map[string]interface{}{},
		R:    false,
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `( origin.name | "unknown" ) == "users"`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"origin.name": "users",
		},
		R:    true,
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `request.header["user"] | "unknown"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"request.header": map[string]string{
				"myheader": "bbb",
			},
		},
		R:    "unknown",
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `origin.name | "users"`,
		Type: descriptor.STRING,
		I:    map[string]interface{}{},
		R:    "users",
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E: `(x/y) == 30`,
		I: map[string]interface{}{
			"x": int64(20),
			"y": int64(10),
		},
		CompileErr: "unknown function: QUO",
		AstErr:     "unknown function: QUO",
		conf:       TestConfigs["Expr/Eval"],
	},
	{
		E:    `request.header["X-FORWARDED-HOST"] == "aaa"`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"request.header": map[string]string{
				"X-FORWARDED-HOST": "bbb",
			},
		},
		R:    false,
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `request.header["X-FORWARDED-HOST"] == "aaa"`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"request.header1": map[string]string{
				"X-FORWARDED-HOST": "bbb",
			},
		},
		Err:    "lookup failed: 'request.header'",
		AstErr: "unresolved attribute",
		conf:   TestConfigs["Expr/Eval"],
	},
	{
		E:    `request.header[headername] == "aaa"`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"request.header": map[string]string{
				"X-FORWARDED-HOST": "bbb",
			},
		},
		Err:    "lookup failed: 'headername'",
		AstErr: "unresolved attribute",
		conf:   TestConfigs["Expr/Eval"],
	},
	{
		E:    `request.header[headername] == "aaa"`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"request.header": map[string]string{
				"X-FORWARDED-HOST": "aaa",
			},
			"headername": "X-FORWARDED-HOST",
		},
		R:    true,
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `match(service.name, "*.ns1.cluster")`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"service.name": "svc1.ns1.cluster",
		},
		R:    true,
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `match(service.name, "*.ns1.cluster")`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"service.name": "svc1.ns2.cluster",
		},
		R:    false,
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `match(service.name, "*.ns1.cluster")`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"service.name": 20,
		},
		Err:    "error converting value to string: '20'", // runtime error
		AstErr: "input 'str' to 'match' func was not a string",
		conf:   TestConfigs["Expr/Eval"],
	},
	{
		E:    `match(service.name, servicename)`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"service.name1": "svc1.ns2.cluster",
			"servicename":   "*.aaa",
		},
		Err:    "lookup failed: 'service.name'",
		AstErr: "unresolved attribute",
		conf:   TestConfigs["Expr/Eval"],
	},
	{
		E:    `match(service.name, servicename)`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"service.name": "svc1.ns2.cluster",
		},
		Err:    "lookup failed: 'servicename'",
		AstErr: "unresolved attribute",
		conf:   TestConfigs["Expr/Eval"],
	},
	{
		E: `match(service.name, 1)`,
		I: map[string]interface{}{
			"service.name": "svc1.ns2.cluster",
		},
		CompileErr: "match($service.name, 1) arg 2 (1) typeError got INT64, expected STRING",
		AstErr:     "input 'pattern' to 'match' func was not a string",
		conf:       TestConfigs["Expr/Eval"],
	},
	{
		E:    `target.ip| ip("10.1.12.3")`,
		Type: descriptor.IP_ADDRESS,
		I:    map[string]interface{}{},
		R:    []uint8(net.ParseIP("10.1.12.3")),
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `target.ip| ip(2)`,
		Type: descriptor.IP_ADDRESS,
		I: map[string]interface{}{
			"target.ip": "",
		},
		CompileErr: "ip(2) arg 1 (2) typeError got INT64, expected STRING",
		AstErr:     "input to 'ip' func was not a string",
		conf:       TestConfigs["Expr/Eval"],
	},
	{
		E:      `target.ip| ip("10.1.12")`,
		Type:   descriptor.IP_ADDRESS,
		I:      map[string]interface{}{},
		Err:    "could not convert 10.1.12 to IP_ADDRESS",
		AstErr: "could not convert '10.1.12' to IP_ADDRESS",
		conf:   TestConfigs["Expr/Eval"],
	},
	{
		E:    `request.time | timestamp("2015-01-02T15:04:35Z")`,
		Type: descriptor.TIMESTAMP,
		I:    map[string]interface{}{},
		R:    t,
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    `request.time | timestamp(2)`,
		Type: descriptor.TIMESTAMP,
		I: map[string]interface{}{
			"request.time": "",
		},
		CompileErr: "timestamp(2) arg 1 (2) typeError got INT64, expected STRING",
		AstErr:     "input to 'timestamp' func was not a string",
		conf:       TestConfigs["Expr/Eval"],
	},
	{
		E:      `request.time | timestamp("242233")`,
		Type:   descriptor.TIMESTAMP,
		I:      map[string]interface{}{},
		Err:    "could not convert '242233' to TIMESTAMP. expected format: '" + time.RFC3339 + "'",
		AstErr: "could not convert '242233' to TIMESTAMP. expected format: '" + time.RFC3339 + "'",
		conf:   TestConfigs["Expr/Eval"],
	},

	// Tests from expr/eval_test.go TestCEXLEval
	{
		E: "a = 2",
		I: map[string]interface{}{
			"a": int64(2),
		},
		CompileErr: "unable to parse expression 'a = 2': 1:3: expected '==', found '='",
		AstErr:     "unable to parse",
		conf:       TestConfigs["Expr/Eval"],
	},
	{
		E:    "a == 2",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:    true,
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    "a == 3",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:    false,
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:    "a == 2",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:    true,
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:      "a == 2",
		Type:   descriptor.BOOL,
		I:      map[string]interface{}{},
		Err:    "lookup failed: 'a'",
		AstErr: "unresolved attribute",
		conf:   TestConfigs["Expr/Eval"],
	},
	{
		E:    `request.user | "user1"`,
		Type: descriptor.STRING,
		I:    map[string]interface{}{},
		R:    "user1",
		conf: TestConfigs["Expr/Eval"],
	},
	{
		E:      "a == 2",
		Type:   descriptor.BOOL,
		I:      map[string]interface{}{},
		Err:    "lookup failed: 'a'",
		AstErr: "unresolved attribute",
		conf:   TestConfigs["Expr/Eval"],
	},

	// Tests from compiler/compiler_test.go
	{
		E:    "true",
		Type: descriptor.BOOL,
		R:    true,
		IL: `
fn eval() bool
  apush_b true
  ret
end`,
	},
	{
		E:    "false",
		Type: descriptor.BOOL,
		R:    false,
		IL: `
fn eval() bool
  apush_b false
  ret
end`,
	},
	{
		E:    `"ABC"`,
		Type: descriptor.STRING,
		R:    "ABC",
		IL: `
fn eval() string
  apush_s "ABC"
  ret
end`,
	},
	{
		E:    `456789`,
		Type: descriptor.INT64,
		R:    int64(456789),
		IL: `
fn eval() integer
  apush_i 456789
  ret
end`,
	},
	{
		E:    `456.789`,
		Type: descriptor.DOUBLE,
		R:    float64(456.789),
		IL: `
fn eval() double
  apush_d 456.789000
  ret
end`,
	},
	{
		E:    `true || false`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `
fn eval() bool
  apush_b true
  jz L0
  apush_b true
  ret
L0:
  apush_b false
  ret
end`,
	},

	{
		E:    `false || true`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `
fn eval() bool
  apush_b false
  jz L0
  apush_b true
  ret
L0:
  apush_b true
  ret
end`,
	},

	{
		E:    `false || false`,
		Type: descriptor.BOOL,
		R:    false,
	},
	{
		E:    `true || false`,
		Type: descriptor.BOOL,
		R:    true,
	},
	{
		E:    `false || true`,
		Type: descriptor.BOOL,
		R:    true,
	},

	{
		E:    `false || true || false`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `
fn eval() bool
  apush_b false
  jz L0
  apush_b true
  jmp L1
L0:
  apush_b true
L1:
  jz L2
  apush_b true
  ret
L2:
  apush_b false
  ret
end`,
	},
	{
		E:    `false || false || false`,
		Type: descriptor.BOOL,
		R:    false,
	},
	{
		E:    `false && true`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `
fn eval() bool
  apush_b false
  apush_b true
  and
  ret
end`,
	},
	{
		E:    `true && false`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `
 fn eval() bool
  apush_b true
  apush_b false
  and
  ret
end`,
	},
	{
		E:    `true && true`,
		Type: descriptor.BOOL,
		R:    true,
	},
	{
		E:    `false && false`,
		Type: descriptor.BOOL,
		R:    false,
	},
	{
		E:    "b1 && b2",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"b1": false,
			"b2": true,
		},
		R: false,
	},
	{
		E:    "b1 && b2",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"b1": true,
			"b2": true,
		},
		R: true,
	},
	{
		E:    "3 == 2",
		Type: descriptor.BOOL,
		R:    false,
		IL: `
fn eval() bool
  apush_i 3
  aeq_i 2
  ret
end`,
	},

	{
		E:    "true == false",
		Type: descriptor.BOOL,
		R:    false,
		IL: `
fn eval() bool
  apush_b true
  aeq_b false
  ret
end`,
	},

	{
		E:    "false == false",
		Type: descriptor.BOOL,
		R:    true,
	},
	{
		E:    "false == true",
		Type: descriptor.BOOL,
		R:    false,
	},
	{
		E:    "true == true",
		Type: descriptor.BOOL,
		R:    true,
	},

	{
		E:    `"ABC" == "ABC"`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `
fn eval() bool
  apush_s "ABC"
  aeq_s "ABC"
  ret
end`,
	},

	{
		E:    `"ABC" == "CBA"`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `
fn eval() bool
  apush_s "ABC"
  aeq_s "CBA"
  ret
end`,
	},

	{
		E:    `23.45 == 45.23`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `
fn eval() bool
  apush_d 23.450000
  aeq_d 45.230000
  ret
end`,
	},

	{
		E:    "3 != 2",
		Type: descriptor.BOOL,
		R:    true,
		IL: `
fn eval() bool
  apush_i 3
  aeq_i 2
  not
  ret
end`,
	},

	{
		E:    "true != false",
		Type: descriptor.BOOL,
		R:    true,
		IL: `
fn eval() bool
  apush_b true
  aeq_b false
  not
  ret
end`,
	},

	{
		E:    `"ABC" != "ABC"`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `
fn eval() bool
  apush_s "ABC"
  aeq_s "ABC"
  not
  ret
end`,
	},

	{
		E:    `23.45 != 45.23`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `
fn eval() bool
  apush_d 23.450000
  aeq_d 45.230000
  not
  ret
end`,
	},
	{
		E:    `ab`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"ab": true,
		},
		R: true,
		IL: `
fn eval() bool
  resolve_b "ab"
  ret
end`,
	},
	{
		E:    `ab`,
		Type: descriptor.BOOL,
		Err:  "lookup failed: 'ab'",
		R:    true, // Keep the return type, so that the special-purpose methods can be tested.
	},
	{
		E:    `as`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"as": "AAA",
		},
		R: "AAA",
		IL: `
fn eval() string
  resolve_s "as"
  ret
end`,
	},
	{
		E:    `ad`,
		Type: descriptor.DOUBLE,
		I: map[string]interface{}{
			"ad": float64(23.46),
		},
		R: float64(23.46),
		IL: `
fn eval() double
  resolve_d "ad"
  ret
end`,
	},
	{
		E:    `ai`,
		Type: descriptor.INT64,
		I: map[string]interface{}{
			"ai": int64(2346),
		},
		R: int64(2346),
		IL: `
fn eval() integer
  resolve_i "ai"
  ret
end`,
	},
	{
		E:    `ar["b"]`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"ar": map[string]string{
				"b": "c",
			},
		},
		R: "c",
		IL: `
fn eval() string
  resolve_f "ar"
  anlookup "b"
  ret
end
`,
	},
	{
		E:    `ai == 20 || ar["b"] == "c"`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"ai": int64(19),
			"ar": map[string]string{
				"b": "c",
			},
		},
		R: true,
		IL: `
fn eval() bool
  resolve_i "ai"
  aeq_i 20
  jz L0
  apush_b true
  ret
L0:
  resolve_f "ar"
  anlookup "b"
  aeq_s "c"
  ret
end`,
	},
	{
		E:       `as | ""`,
		Type:    descriptor.STRING,
		R:       "",
		SkipAst: true, // AST returns nil for this case.
	},
	{
		E:    `as | ""`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"as": "foo",
		},
		R: "foo",
	},
	{
		E:    `as | "user1"`,
		Type: descriptor.STRING,
		R:    "user1",
		IL: `
fn eval() string
  tresolve_s "as"
  jnz L0
  apush_s "user1"
L0:
  ret
end`,
	},
	{
		E:    `as | "user1"`,
		Type: descriptor.STRING,
		R:    "a2",
		I: map[string]interface{}{
			"as": "a2",
		},
	},
	{
		E:    `as | bs | "user1"`,
		Type: descriptor.STRING,
		R:    "user1",
		IL: `
fn eval() string
  tresolve_s "as"
  jnz L0
  tresolve_s "bs"
  jnz L0
  apush_s "user1"
L0:
  ret
end`,
	},
	{
		E:    `as | bs | "user1"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"as": "a2",
		},
		R: "a2",
	},
	{
		E:    `as | bs | "user1"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"bs": "b2",
		},
		R: "b2",
	},
	{
		E:    `as | bs | "user1"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"as": "a2",
			"bs": "b2",
		},
		R: "a2",
	},

	{
		E:    `ab | true`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"ab": false,
		},
		R: false,
		IL: `
 fn eval() bool
  tresolve_b "ab"
  jnz L0
  apush_b true
L0:
  ret
end`,
	},
	{
		E:    `ab | true`,
		Type: descriptor.BOOL,
		I:    map[string]interface{}{},
		R:    true,
	},
	{
		E:    `ab | bb | true`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"ab": false,
		},
		R: false,
		IL: `
fn eval() bool
  tresolve_b "ab"
  jnz L0
  tresolve_b "bb"
  jnz L0
  apush_b true
L0:
  ret
end`,
	},
	{
		E:    `ab | bb | true`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"bb": false,
		},
		R: false,
	},
	{
		E:    `ab | bb | true`,
		Type: descriptor.BOOL,
		I:    map[string]interface{}{},
		R:    true,
	},

	{
		E:    `ai | 42`,
		Type: descriptor.INT64,
		I: map[string]interface{}{
			"ai": int64(10),
		},
		R: int64(10),
		IL: `
fn eval() integer
  tresolve_i "ai"
  jnz L0
  apush_i 42
L0:
  ret
end`,
	},
	{
		E:    `ai | 42`,
		Type: descriptor.INT64,
		I:    map[string]interface{}{},
		R:    int64(42),
	},
	{
		E:    `ai | bi | 42`,
		Type: descriptor.INT64,
		I: map[string]interface{}{
			"ai": int64(10),
		},
		R: int64(10),
		IL: `
fn eval() integer
  tresolve_i "ai"
  jnz L0
  tresolve_i "bi"
  jnz L0
  apush_i 42
L0:
  ret
end`,
	},
	{
		E:    `ai | bi | 42`,
		Type: descriptor.INT64,
		I: map[string]interface{}{
			"bi": int64(20),
		},
		R: int64(20),
	},
	{
		E:    `ai | bi | 42`,
		Type: descriptor.INT64,
		I:    map[string]interface{}{},
		R:    int64(42),
	},

	{
		E:    `ad | 42.1`,
		Type: descriptor.DOUBLE,
		I: map[string]interface{}{
			"ad": float64(10),
		},
		R: float64(10),
		IL: `
fn eval() double
  tresolve_d "ad"
  jnz L0
  apush_d 42.100000
L0:
  ret
end`,
	},
	{
		E:    `ad | 42.1`,
		Type: descriptor.DOUBLE,
		I:    map[string]interface{}{},
		R:    float64(42.1),
	},
	{
		E:    `ad | bd | 42.1`,
		Type: descriptor.DOUBLE,
		I: map[string]interface{}{
			"ad": float64(10),
		},
		R: float64(10),
		IL: `
fn eval() double
  tresolve_d "ad"
  jnz L0
  tresolve_d "bd"
  jnz L0
  apush_d 42.100000
L0:
  ret
end`,
	},
	{
		E:    `ad | bd | 42.1`,
		Type: descriptor.DOUBLE,
		I: map[string]interface{}{
			"bd": float64(20),
		},
		R: float64(20),
	},
	{
		E:    `ad | bd | 42.1`,
		Type: descriptor.DOUBLE,
		I:    map[string]interface{}{},
		R:    float64(42.1),
	},

	{
		E:       `(ar | br)["foo"]`,
		Type:    descriptor.STRING,
		SkipAst: true, // AST evaluator panics for this case.
		I: map[string]interface{}{
			"ar": map[string]string{
				"foo": "bar",
			},
			"br": map[string]string{
				"foo": "far",
			},
		},
		R: "bar",
		IL: `
fn eval() string
  tresolve_f "ar"
  jnz L0
  resolve_f "br"
L0:
  anlookup "foo"
  ret
end`,
	},
	{
		E:       `(ar | br)["foo"]`,
		Type:    descriptor.STRING,
		SkipAst: true, // AST evaluator panics for this case.
		I: map[string]interface{}{
			"br": map[string]string{
				"foo": "far",
			},
		},
		R: "far",
	},

	{
		E:    "ai == 2",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"ai": int64(2),
		},
		R: true,
		IL: `
 fn eval() bool
  resolve_i "ai"
  aeq_i 2
  ret
end`,
	},
	{
		E:    "ai == 2",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"ai": int64(0x7F000000FF000000),
		},
		R: false,
	},
	{
		E:    "as == bs",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"as": "ABC",
			"bs": "ABC",
		},
		R: true,
		IL: `
fn eval() bool
  resolve_s "as"
  resolve_s "bs"
  eq_s
  ret
end`,
	},
	{
		E:    "ab == bb",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"ab": true,
			"bb": true,
		},
		R: true,
		IL: `
fn eval() bool
  resolve_b "ab"
  resolve_b "bb"
  eq_b
  ret
end`,
	},
	{
		E:    "ai == bi",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"ai": int64(0x7F000000FF000000),
			"bi": int64(0x7F000000FF000000),
		},
		R: true,
		IL: `
fn eval() bool
  resolve_i "ai"
  resolve_i "bi"
  eq_i
  ret
end`,
	},
	{
		E:    "ad == bd",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"ad": float64(345345.45),
			"bd": float64(345345.45),
		},
		R: true,
		IL: `
fn eval() bool
  resolve_d "ad"
  resolve_d "bd"
  eq_d
  ret
end`,
	},
	{
		E:    "ai != 2",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"ai": int64(2),
		},
		R: false,
		IL: `
 fn eval() bool
  resolve_i "ai"
  aeq_i 2
  not
  ret
end`,
	},

	{
		E:    `sm["foo"]`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"sm": map[string]string{"foo": "bar"},
		},
		R: "bar",
		IL: `
fn eval() string
  resolve_f "sm"
  anlookup "foo"
  ret
end`,
	},
	{
		E:    `sm[as]`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"as": "foo",
			"sm": map[string]string{"foo": "bar"},
		},
		R: "bar",
		IL: `
fn eval() string
  resolve_f "sm"
  resolve_s "as"
  nlookup
  ret
end`,
	},
	{
		E:    `ar["c"] | "foo"`,
		Type: descriptor.STRING,
		I:    map[string]interface{}{},
		R:    "foo",
		IL: `
fn eval() string
  tresolve_f "ar"
  jnz L0
  jmp L1
L0:
  apush_s "c"
  tlookup
  jnz L2
L1:
  apush_s "foo"
L2:
  ret
end`,
	},
	{
		E:    `ar["c"] | "foo"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"ar": map[string]string{"c": "b"},
		},
		R: "b",
	},
	{
		E:    `ar[as] | "foo"`,
		Type: descriptor.STRING,
		I:    map[string]interface{}{},
		R:    "foo",
		IL: `
fn eval() string
  tresolve_f "ar"
  jnz L0
  jmp L1
L0:
  tresolve_s "as"
  jnz L2
  jmp L1
L2:
  tlookup
  jnz L3
L1:
  apush_s "foo"
L3:
  ret
end`,
	},
	{
		E:    `ar[as] | "foo"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"ar": map[string]string{"as": "bar"},
		},
		R: "foo",
	},
	{
		E:    `ar[as] | "foo"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"as": "bar",
		},
		R: "foo",
	},
	{
		E:    `ar[as] | "foo"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"ar": map[string]string{"as": "bar"},
			"as": "!!!!",
		},
		R: "foo",
	},
	{
		E:    `ar[as] | "foo"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"ar": map[string]string{"asval": "bar"},
			"as": "asval",
		},
		R: "bar",
	},
	{
		E:    `ar["b"] | ar["c"] | "null"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"ar": map[string]string{
				"b": "c",
				"c": "b",
			},
		},
		R: "c",
		IL: `
fn eval() string
  tresolve_f "ar"
  jnz L0
  jmp L1
L0:
  apush_s "b"
  tlookup
  jnz L2
L1:
  tresolve_f "ar"
  jnz L3
  jmp L4
L3:
  apush_s "c"
  tlookup
  jnz L2
L4:
  apush_s "null"
L2:
  ret
end`,
	},
	{
		E:    `ar["b"] | ar["c"] | "null"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"ar": map[string]string{},
		},
		R: "null",
	},
	{
		E:    `ar["b"] | ar["c"] | "null"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"ar": map[string]string{
				"b": "c",
			},
		},
		R: "c",
	},
	{
		E:    `ar["b"] | ar["c"] | "null"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"ar": map[string]string{
				"c": "b",
			},
		},
		R: "b",
	},
	{
		E:    `adur`,
		Type: descriptor.DURATION,
		I: map[string]interface{}{
			"adur": duration20,
		},
		R: duration20,
		IL: `
fn eval() duration
  resolve_i "adur"
  ret
end`,
	},
	{
		E:    `adur | "19ms"`,
		Type: descriptor.DURATION,
		I:    map[string]interface{}{},
		R:    duration19,
		IL: `
fn eval() duration
  tresolve_i "adur"
  jnz L0
  apush_i 19000000
L0:
  ret
end`,
	},
	{
		E:    `adur | "19ms"`,
		Type: descriptor.DURATION,
		I: map[string]interface{}{
			"adur": duration20,
		},
		R: duration20,
	},
	{
		E:    `at`,
		Type: descriptor.TIMESTAMP,
		I: map[string]interface{}{
			"at": time1977,
		},
		R: time1977,
	},
	{
		E:    `at | bt`,
		Type: descriptor.TIMESTAMP,
		I: map[string]interface{}{
			"at": time1999,
			"bt": time1977,
		},
		R: time1999,
	},
	{
		E:    `at | bt`,
		Type: descriptor.TIMESTAMP,
		I: map[string]interface{}{
			"bt": time1977,
		},
		R: time1977,
	},
	{
		E:    `aip`,
		Type: descriptor.IP_ADDRESS,
		I: map[string]interface{}{
			"aip": []byte{0x1, 0x2, 0x3, 0x4},
		},
		R: []byte{0x1, 0x2, 0x3, 0x4},
	},
	{
		E:    `aip | bip`,
		Type: descriptor.IP_ADDRESS,
		I: map[string]interface{}{
			"bip": []byte{0x4, 0x5, 0x6, 0x7},
		},
		R: []byte{0x4, 0x5, 0x6, 0x7},
	},
	{
		E:    `aip | bip`,
		Type: descriptor.IP_ADDRESS,
		I: map[string]interface{}{
			"aip": []byte{0x1, 0x2, 0x3, 0x4},
			"bip": []byte{0x4, 0x5, 0x6, 0x7},
		},
		R: []byte{0x1, 0x2, 0x3, 0x4},
	},
	{
		E:    `ip("0.0.0.0")`,
		Type: descriptor.IP_ADDRESS,
		R:    []byte(net.IPv4zero),
		IL: `fn eval() interface
  apush_s "0.0.0.0"
  call ip
  ret
end`,
	},
	{
		E:    `aip == bip`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"aip": []byte{0x1, 0x2, 0x3, 0x4},
			"bip": []byte{0x4, 0x5, 0x6, 0x7},
		},
		R: false,
		IL: `fn eval() bool
  resolve_f "aip"
  resolve_f "bip"
  call ip_equal
  ret
end`,
	},
	{
		E:    `timestamp("2015-01-02T15:04:35Z")`,
		Type: descriptor.TIMESTAMP,
		R:    t,
		IL: `fn eval() interface
  apush_s "2015-01-02T15:04:35Z"
  call timestamp
  ret
end`,
	},
	{
		E:    `t1 == t2`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"t1": t,
			"t2": t2,
		},
		R: false,
		IL: `fn eval() bool
  resolve_f "t1"
  resolve_f "t2"
  call timestamp_equal
  ret
end`,
	},
	{
		E:    `t1 == t2`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"t1": t,
			"t2": t,
		},
		R: true,
		IL: `fn eval() bool
  resolve_f "t1"
  resolve_f "t2"
  call timestamp_equal
  ret
end`,
	},
	{
		E:    `t1 != t2`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"t1": t,
			"t2": t,
		},
		R: false,
		IL: `fn eval() bool
  resolve_f "t1"
  resolve_f "t2"
  call timestamp_equal
  not
  ret
end`,
	},
	{
		E:    `t1 | t2`,
		Type: descriptor.TIMESTAMP,
		I: map[string]interface{}{
			"t2": t2,
		},
		R: t2,
	},
	{
		E:    `t1 | t2`,
		Type: descriptor.TIMESTAMP,
		I: map[string]interface{}{
			"t1": t,
			"t2": t2,
		},
		R: t,
	},
	{
		E:          `@23`,
		CompileErr: "unable to parse expression '@23': 1:1: illegal character U+0040 '@'",
		AstErr:     "unable to parse expression '@23': 1:1: illegal character U+0040 '@'",
	},
	{
		E:          `ai == true`,
		CompileErr: "EQ($ai, true) arg 2 (true) typeError got BOOL, expected INT64",
		AstErr:     "unresolved attribute ai",
	},

	{
		E:    `"foo" | "bar"`,
		Type: descriptor.STRING,
		IL: `
fn eval() string
  apush_s "foo"
  jmp L0
  apush_s "bar"
L0:
  ret
end
		`,
		R: "foo",
	},

	{
		E:    `ip("1.2.3.4")`,
		Type: descriptor.IP_ADDRESS,
		IL: `
fn eval() interface
  apush_s "1.2.3.4"
  call ip
  ret
end
		`,
		R: []uint8(net.ParseIP("1.2.3.4")),
	},

	{
		E:    `ip(as)`,
		Type: descriptor.IP_ADDRESS,
		I: map[string]interface{}{
			"as": "1.2.3.4",
		},
		IL: `
fn eval() interface
  resolve_s "as"
  call ip
  ret
end
		`,
		R: []uint8(net.ParseIP("1.2.3.4")),
	},

	{
		E:    `ip("1.2.3.4" | "5.6.7.8")`,
		Type: descriptor.IP_ADDRESS,
		IL: `
fn eval() interface
  apush_s "1.2.3.4"
  jmp L0
  apush_s "5.6.7.8"
L0:
  call ip
  ret
end
		`,
		R: []uint8(net.ParseIP("1.2.3.4")),
	},

	{
		E:    `ip(as | "5.6.7.8")`,
		Type: descriptor.IP_ADDRESS,
		IL: `
fn eval() interface
  tresolve_s "as"
  jnz L0
  apush_s "5.6.7.8"
L0:
  call ip
  ret
end
`,
		R: []uint8(net.ParseIP("5.6.7.8")),
	},

	{
		E:    `ip(as | bs)`,
		Type: descriptor.IP_ADDRESS,
		I: map[string]interface{}{
			"bs": "1.2.3.4",
		},
		R: []uint8(net.ParseIP("1.2.3.4")),
	},

	{
		E:    `ip(as | bs)`,
		Type: descriptor.IP_ADDRESS,
		Err:  "lookup failed: 'bs'",
	},

	{
		E:    `ip(ar["foo"])`,
		Type: descriptor.IP_ADDRESS,
		IL: `
fn eval() interface
  resolve_f "ar"
  anlookup "foo"
  call ip
  ret
end
`,
		I: map[string]interface{}{
			"ar": map[string]string{"foo": "1.2.3.4"},
		},
		R: []uint8(net.ParseIP("1.2.3.4")),
	},

	{
		E:    `reverse(as)`,
		Type: descriptor.STRING,
		IL: `
fn eval() string
  resolve_s "as"
  call reverse
  ret
end
`,
		I: map[string]interface{}{
			"as": "str1",
		},
		R: "1rts",
		Fns: []expr.FunctionMetadata{
			{Name: "reverse", Instance: false, ArgumentTypes: []descriptor.ValueType{descriptor.STRING}, ReturnType: descriptor.STRING},
		},
		Externs: map[string]interface{}{
			"reverse": func(s string) string {
				runes := []rune(s)
				for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
					runes[i], runes[j] = runes[j], runes[i]
				}
				return string(runes)
			},
		},
	},

	{
		E:    `as.reverse()`,
		Type: descriptor.STRING,
		IL: `
fn eval() string
  resolve_s "as"
  call reverse
  ret
end
`,
		I: map[string]interface{}{
			"as": "str1",
		},
		R: "1rts",
		Fns: []expr.FunctionMetadata{
			{Name: "reverse", Instance: true, TargetType: descriptor.STRING, ReturnType: descriptor.STRING},
		},
		Externs: map[string]interface{}{
			"reverse": func(s string) string {
				runes := []rune(s)
				for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
					runes[i], runes[j] = runes[j], runes[i]
				}
				return string(runes)
			},
		},
	},

	{
		E:          `"aaa".matches(23)`,
		CompileErr: `"aaa":matches(23) arg 1 (23) typeError got INT64, expected STRING`,
	},
	{
		E:          `matches("aaa")`,
		CompileErr: `invoking instance method without an instance: matches`,
	},
	{
		E:          `matches()`,
		CompileErr: `invoking instance method without an instance: matches`,
	},
	{
		E:    `"abc".matches("abc")`,
		Type: descriptor.BOOL,
		IL: `
fn eval() bool
  apush_s "abc"
  apush_s "abc"
  call matches
  ret
end
`,
		R: true,
	},
	{
		E:    `".*".matches("abc")`,
		Type: descriptor.BOOL,
		IL: `
fn eval() bool
  apush_s ".*"
  apush_s "abc"
  call matches
  ret
end
`,
		R: true,
	},
	{
		E:    `"ab.*d".matches("abc")`,
		Type: descriptor.BOOL,
		R:    false,
	},
	{
		E:    `as.matches(bs)`,
		Type: descriptor.BOOL,
		IL: `
fn eval() bool
  resolve_s "as"
  resolve_s "bs"
  call matches
  ret
end`,
		I: map[string]interface{}{
			"as": "st.*",
			"bs": "str1",
		},
		R: true,
	},
	{
		E:    `as.matches(bs)`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"as": "st.*",
			"bs": "sqr1",
		},
		R: false,
	},

	{
		E:          `"aaa".startsWith(23)`,
		CompileErr: `"aaa":startsWith(23) arg 1 (23) typeError got INT64, expected STRING`,
	},
	{
		E:          `startsWith("aaa")`,
		CompileErr: `invoking instance method without an instance: startsWith`,
	},
	{
		E:          `startsWith()`,
		CompileErr: `invoking instance method without an instance: startsWith`,
	},
	{
		E:    `"abc".startsWith("abc")`,
		Type: descriptor.BOOL,
		IL: `
fn eval() bool
  apush_s "abc"
  apush_s "abc"
  call startsWith
  ret
end
`,
		R: true,
	},
	{
		E:    `"abcd".startsWith("abc")`,
		Type: descriptor.BOOL,
		IL: `
fn eval() bool
  apush_s "abcd"
  apush_s "abc"
  call startsWith
  ret
end
`,
		R: true,
	},
	{
		E:    `"abfood".startsWith("abc")`,
		Type: descriptor.BOOL,
		R:    false,
	},
	{
		E:    `as.startsWith(bs)`,
		Type: descriptor.BOOL,
		IL: `
fn eval() bool
  resolve_s "as"
  resolve_s "bs"
  call startsWith
  ret
end`,
		I: map[string]interface{}{
			"as": "abcd",
			"bs": "ab",
		},
		R: true,
	},
	{
		E:    `as.startsWith(bs)`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"as": "bcda",
			"bs": "abc",
		},
		R: false,
	},

	{
		E:          `"aaa".endsWith(23)`,
		CompileErr: `"aaa":endsWith(23) arg 1 (23) typeError got INT64, expected STRING`,
	},
	{
		E:          `endsWith("aaa")`,
		CompileErr: `invoking instance method without an instance: endsWith`,
	},
	{
		E:          `endsWith()`,
		CompileErr: `invoking instance method without an instance: endsWith`,
	},
	{
		E:    `"abc".endsWith("abc")`,
		Type: descriptor.BOOL,
		IL: `
fn eval() bool
  apush_s "abc"
  apush_s "abc"
  call endsWith
  ret
end
`,
		R: true,
	},
	{
		E:    `"abcd".endsWith("bcd")`,
		Type: descriptor.BOOL,
		IL: `
fn eval() bool
  apush_s "abcd"
  apush_s "bcd"
  call endsWith
  ret
end
`,
		R: true,
	},
	{
		E:    `"abfood".endsWith("abc")`,
		Type: descriptor.BOOL,
		R:    false,
	},
	{
		E:    `as.endsWith(bs)`,
		Type: descriptor.BOOL,
		IL: `
fn eval() bool
  resolve_s "as"
  resolve_s "bs"
  call endsWith
  ret
end`,
		I: map[string]interface{}{
			"as": "abcd",
			"bs": "cd",
		},
		R: true,
	},
	{
		E:    `as.endsWith(bs)`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"as": "bcda",
			"bs": "abc",
		},
		R: false,
	},
}

// TestInfo is a structure that contains detailed test information. Depending
// on the test type, various fields of the TestInfo struct will be used for
// testing purposes. For example, compiler can use E and IL to test
// expression => IL conversion, interpreter can use IL and I, R&Err for evaluation
// tests and the evaluator can use expression and I, R&Err to test evaluation.
type TestInfo struct {
	// name is the explicit name supplied for the test. If it is not supplied, then the expression will be used as name.
	name string

	// E contains the expression that is being tested.
	E string

	// IL contains the textual IL representation of code.
	IL string

	// I contains the attribute bag used for testing.
	I map[string]interface{}

	// R contains the expected result of a successful evaluation.
	R interface{}

	// Type is the expected of the expression upon successful compilation.
	Type descriptor.ValueType

	// Err contains the expected error message of a failed evaluation.
	Err string

	// AstErr contains the expected error message of a failed evaluation, during AST evaluation.
	AstErr string

	// CompileErr contains the expected error message for a failed compilation.
	CompileErr string

	// Config field holds the GlobalConfig to use when compiling/evaluating the tests.
	// If nil, then "Default" config will be used.
	conf *pb.GlobalConfig

	// Fns field holds any additional function metadata that needs to be involved in the test.
	Fns []expr.FunctionMetadata

	// Externs holds any additional externs that should be used during evaluation.
	Externs map[string]interface{}

	// SkipAst indicates that AST based evaluator should not be used for this test.
	SkipAst bool

	// Use this test as a benchmark as well.
	Bench bool
}

// TestName is the name to use for the test.
func (t *TestInfo) TestName() string {
	if t.name != "" {
		return t.name
	}

	return t.E
}

// Conf returns the global config to use for the test.
func (t *TestInfo) Conf() *pb.GlobalConfig {
	if t.conf != nil {
		return t.conf
	}
	return TestConfigs["Default"]
}

// CheckEvaluationResult compares the given evaluation result and error against the one that is declared in test.
// Returns an error if there is a mismatch.
func (t *TestInfo) CheckEvaluationResult(r interface{}, err error) error {
	if t.Err != "" {
		if err == nil {
			return fmt.Errorf("expected error was not found: '%v'", t.Err)
		}
		if !strings.HasPrefix(err.Error(), t.Err) {
			return fmt.Errorf("evaluation error mismatch: '%v' != '%v'", err.Error(), t.Err)
		}

		return nil
	}

	if err != nil {
		return fmt.Errorf("unexpected evaluation error: '%v'", err)
	}

	if !AreEqual(t.R, r) {
		return fmt.Errorf("evaluation result mismatch: '%v' != '%v'", r, t.R)
	}

	return nil
}

// TestConfigs uses a standard set of configs to use when executing tests.
var TestConfigs = map[string]*pb.GlobalConfig{
	"Expr/Eval": {
		Manifests: []*pb.AttributeManifest{
			{
				Attributes: map[string]*pb.AttributeManifest_AttributeInfo{
					"a": {
						ValueType: descriptor.INT64,
					},
					"request.user": {
						ValueType: descriptor.STRING,
					},
					"request.user2": {
						ValueType: descriptor.STRING,
					},
					"request.user3": {
						ValueType: descriptor.STRING,
					},
					"source.name": {
						ValueType: descriptor.STRING,
					},
					"source.target": {
						ValueType: descriptor.STRING,
					},
					"request.size": {
						ValueType: descriptor.INT64,
					},
					"request.size1": {
						ValueType: descriptor.INT64,
					},
					"x": {
						ValueType: descriptor.INT64,
					},
					"y": {
						ValueType: descriptor.INT64,
					},
					"service.name": {
						ValueType: descriptor.STRING,
					},
					"service.user": {
						ValueType: descriptor.STRING,
					},
					"origin.name": {
						ValueType: descriptor.STRING,
					},
					"request.header": {
						ValueType: descriptor.STRING_MAP,
					},
					"request.time": {
						ValueType: descriptor.TIMESTAMP,
					},
					"headername": {
						ValueType: descriptor.STRING,
					},
					"target.ip": {
						ValueType: descriptor.IP_ADDRESS,
					},
					"servicename": {
						ValueType: descriptor.STRING,
					},
				},
			},
		},
	},
	"Default": {
		Manifests: []*pb.AttributeManifest{
			{
				Attributes: map[string]*pb.AttributeManifest_AttributeInfo{
					"ai": {
						ValueType: descriptor.INT64,
					},
					"ab": {
						ValueType: descriptor.BOOL,
					},
					"as": {
						ValueType: descriptor.STRING,
					},
					"ad": {
						ValueType: descriptor.DOUBLE,
					},
					"ar": {
						ValueType: descriptor.STRING_MAP,
					},
					"adur": {
						ValueType: descriptor.DURATION,
					},
					"at": {
						ValueType: descriptor.TIMESTAMP,
					},
					"aip": {
						ValueType: descriptor.IP_ADDRESS,
					},
					"bi": {
						ValueType: descriptor.INT64,
					},
					"bb": {
						ValueType: descriptor.BOOL,
					},
					"bs": {
						ValueType: descriptor.STRING,
					},
					"bd": {
						ValueType: descriptor.DOUBLE,
					},
					"br": {
						ValueType: descriptor.STRING_MAP,
					},
					"bdur": {
						ValueType: descriptor.DURATION,
					},
					"bt": {
						ValueType: descriptor.TIMESTAMP,
					},
					"t1": {
						ValueType: descriptor.TIMESTAMP,
					},
					"t2": {
						ValueType: descriptor.TIMESTAMP,
					},
					"bip": {
						ValueType: descriptor.IP_ADDRESS,
					},
					"b1": {
						ValueType: descriptor.BOOL,
					},
					"b2": {
						ValueType: descriptor.BOOL,
					},
					"sm": {
						ValueType: descriptor.STRING_MAP,
					},
				},
			},
		},
	},
}
