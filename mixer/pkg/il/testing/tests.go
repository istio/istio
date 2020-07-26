// Copyright Istio Authors
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
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"time"

	descriptor "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/lang/ast"
	"istio.io/pkg/attribute"
)

var duration19, _ = time.ParseDuration("19ms")
var duration20, _ = time.ParseDuration("20ms")
var time1999 = time.Date(1999, time.December, 31, 23, 59, 0, 0, time.UTC)
var time1977 = time.Date(1977, time.February, 4, 12, 0, 0, 0, time.UTC)
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
		R:          true,
		Referenced: []string{"ai"},
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
		R:          true,
		Referenced: []string{"ai", "ar", "ar[foo]"},
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
		R:          false,
		Referenced: []string{"ai", "ar", "ar[foo]"},
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
		R:          true,
		Referenced: []string{"a"},
		conf:       exprEvalAttrs,
	},
	{
		E:    `a != 2`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:          false,
		Referenced: []string{"a"},
		conf:       exprEvalAttrs,
	},
	{
		E:    `a != 2`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"d": int64(2),
		},
		// top-level idents do not support presence
		CEL:        true,
		Err:        "lookup failed: 'a'",
		AstErr:     "unresolved attribute",
		Referenced: []string{"-a"},
		conf:       exprEvalAttrs,
	},
	{
		E:    "2 != a",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"d": int64(2),
		},
		// top-level idents do not support presence
		CEL:        true,
		Err:        "lookup failed: 'a'",
		AstErr:     "unresolved attribute",
		Referenced: []string{"-a"},
		conf:       exprEvalAttrs,
	},
	{
		E:    "a ",
		Type: descriptor.INT64,
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:          int64(2),
		Referenced: []string{"a"},
		conf:       exprEvalAttrs,
	},

	// Compilation Error due to type mismatch
	{
		E: "true == a",
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:          false,
		CompileErr: "EQ(true, $a) arg 2 ($a) typeError got INT64, expected BOOL",
		conf:       exprEvalAttrs,
	},

	// Compilation Error due to type mismatch
	{
		E: "3.14 == a",
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:          false,
		CompileErr: "EQ(3.14, $a) arg 2 ($a) typeError got INT64, expected DOUBLE",
		conf:       exprEvalAttrs,
	},

	{
		E:    "2 ",
		Type: descriptor.INT64,
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:    int64(2),
		conf: exprEvalAttrs,
	},
	{
		E:    `request.user == "user1"`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"request.user": "user1",
		},
		R:    true,
		conf: exprEvalAttrs,
	},
	{
		E:    `request.user2| request.user | "user1"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"request.user": "user2",
		},
		R:          "user2",
		Referenced: []string{"-request.user2", "request.user"},
		conf:       exprEvalAttrs,
	},
	{
		E:    `request.user2| request.user3 | "user1"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"request.user": "user2",
		},
		R:          "user1",
		Referenced: []string{"-request.user2", "-request.user3"},
		conf:       exprEvalAttrs,
	},
	{
		E:    `request.size| 200`,
		Type: descriptor.INT64,
		I: map[string]interface{}{
			"request.size": int64(120),
		},
		R:          int64(120),
		Referenced: []string{"request.size"},
		conf:       exprEvalAttrs,
	},
	{
		E:    `request.size| 200`,
		Type: descriptor.INT64,
		I: map[string]interface{}{
			"request.size": int64(0),
		},
		R:    int64(0),
		conf: exprEvalAttrs,
	},
	{
		E:    `request.size| 200`,
		Type: descriptor.INT64,
		I: map[string]interface{}{
			"request.size1": int64(0),
		},
		R:    int64(200),
		conf: exprEvalAttrs,
	},
	{
		E:    `(x == 20 && y == 10) || x == 30`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"x": int64(20),
			"y": int64(10),
		},
		R:    true,
		conf: exprEvalAttrs,
	},
	{
		E:    `x == 20 && y == 10`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"a": int64(20),
			"b": int64(10),
		},
		IL: `
 fn eval() bool
  resolve_i "x"
  aeq_i 20
  jz L0
  resolve_i "y"
  aeq_i 10
  jmp L1
L0:
  apush_b false
L1:
  ret
end`,
		// top-level idents do not support presence
		CEL:    false,
		Err:    "lookup failed: 'x'",
		AstErr: "unresolved attribute",
		conf:   exprEvalAttrs,
	},
	{
		E:    `match(service.name, "*.ns1.cluster") && service.user == "admin"`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"service.name": "svc1.ns1.cluster",
			"service.user": "admin",
		},
		R:    true,
		conf: exprEvalAttrs,
	},
	{
		E:    `request.headers[toLower(source.uid)] == "curlish"`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"request.headers": map[string]string{
				"user-agent": "curlish",
			},
			"source.uid": "uSeR-agEnT",
		},
		R:    true,
		conf: istio06AttributeSet,
	},
	{
		E:    `request.headers[toLower("USER-AGENT")] == "curlish"`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"request.headers": map[string]string{
				"user-agent": "curlish",
			},
		},
		R:    true,
		conf: istio06AttributeSet,
		IL: `
fn eval() bool
  resolve_f "request.headers"
  apush_s "USER-AGENT"
  call toLower
  nlookup
  aeq_s "curlish"
  ret
end
`,
	},
	{
		E:    `match(request.headers["user-agent"], "curl*")`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"request.headers": map[string]string{
				"user-agent": "curlish",
			},
		},
		R:    true,
		conf: istio06AttributeSet,
	},
	{
		E:    `match(request.headers["user-agent"], "curl*")`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"request.headers": map[string]string{
				"user-agent": "ishcurl",
			},
		},
		R:    false,
		conf: istio06AttributeSet,
	},
	{
		E:    `match(request.headers["user-agent"], "curl*")`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"request.headers": map[string]string{},
		},
		R: false,
		// CEL produces errors on map lookup
		CEL:  errors.New("no such key"),
		conf: istio06AttributeSet,
	},
	{
		E:    `match(request.headers["user-agent"], "curl*")`,
		Type: descriptor.BOOL,
		I:    map[string]interface{}{},
		Err:  "lookup failed: 'request.headers'",
		conf: istio06AttributeSet,
	},
	{
		E:          `match(request.headerzzzz["user-agent"], "curl*")`,
		CompileErr: "unknown attribute request.headerzzzz",
		conf:       istio06AttributeSet,
	},
	{
		E:    `( origin.name | "unknown" ) == "users"`,
		Type: descriptor.BOOL,
		I:    map[string]interface{}{},
		R:    false,
		conf: exprEvalAttrs,
	},
	{
		E:    `( origin.name | "unknown" ) == "users"`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"origin.name": "users",
		},
		R:    true,
		conf: exprEvalAttrs,
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
		conf: exprEvalAttrs,
	},
	{
		E:    `request.header["user"] | "unknown"`,
		Type: descriptor.STRING,
		R:    "unknown",
		conf: exprEvalAttrs,
	},
	{
		E:    `origin.name | "users"`,
		Type: descriptor.STRING,
		I:    map[string]interface{}{},
		R:    "users",
		conf: exprEvalAttrs,
	},
	{
		E: `(x/y) == 30`,
		I: map[string]interface{}{
			"x": int64(20),
			"y": int64(10),
		},
		CompileErr: "unknown function: QUO",
		AstErr:     "unknown function: QUO",
		conf:       exprEvalAttrs,
	},
	{
		E:    `request.header["X-FORWARDED-HOST"] == "aaa"`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"request.header": map[string]string{
				"X-FORWARDED-HOST": "bbb",
			},
		},
		R:          false,
		Referenced: []string{"request.header", "request.header[X-FORWARDED-HOST]"},
		conf:       exprEvalAttrs,
	},
	{
		E:    `request.header["X-FORWARDED-HOST"] == "aaa"`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"request.header1": map[string]string{
				"X-FORWARDED-HOST": "bbb",
			},
		},
		Referenced: []string{"-request.header"},
		Err:        "lookup failed: 'request.header'",
		AstErr:     "unresolved attribute",
		conf:       exprEvalAttrs,
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
		conf:   exprEvalAttrs,
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
		conf: exprEvalAttrs,
	},
	{
		E:    `match(service.name, "*.ns1.cluster")`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"service.name": "svc1.ns1.cluster",
		},
		R:          true,
		Referenced: []string{"service.name"},
		conf:       exprEvalAttrs,
	},
	{
		E:    `match(service.name, "*.ns1.cluster")`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"service.name": "svc1.ns2.cluster",
		},
		R:    false,
		conf: exprEvalAttrs,
	},
	{
		E:    `match(service.name, "*.ns1.cluster")`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"service.name": 20,
		},
		Err:    "error converting value to string: '20'", // runtime error
		AstErr: "input 'str' to 'match' func was not a string",
		conf:   exprEvalAttrs,
	},
	{
		E:    `match(service.name, servicename)`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"service.name1": "svc1.ns2.cluster",
			"servicename":   "*.aaa",
		},
		Err: "lookup failed: 'service.name'",
		// CEL always resolves attributes
		CEL:        false,
		AstErr:     "unresolved attribute",
		Referenced: []string{"-service.name"},
		// CEL evaluates all arguments to a function since it does not error out on the first lookup failure
		ReferencedCEL: []string{"-service.name", "servicename"},
		conf:          exprEvalAttrs,
	},
	{
		E:    `match(service.name, servicename)`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"service.name": "svc1.ns2.cluster",
		},
		Err: "lookup failed: 'servicename'",
		// CEL always resolves attributes
		CEL:    false,
		AstErr: "unresolved attribute",
		conf:   exprEvalAttrs,
	},
	{
		E: `match(service.name, 1)`,
		I: map[string]interface{}{
			"service.name": "svc1.ns2.cluster",
		},
		CompileErr: "match($service.name, 1) arg 2 (1) typeError got INT64, expected STRING",
		AstErr:     "input 'pattern' to 'match' func was not a string",
		conf:       exprEvalAttrs,
	},
	{
		E:          `destination.ip| ip("10.1.12.3")`,
		Type:       descriptor.IP_ADDRESS,
		I:          map[string]interface{}{},
		R:          []byte(net.ParseIP("10.1.12.3")),
		Referenced: []string{"-destination.ip"},
		conf:       exprEvalAttrs,
	},
	{
		E:    `destination.ip| ip(2)`,
		Type: descriptor.IP_ADDRESS,
		I: map[string]interface{}{
			"destination.ip": "",
		},
		CompileErr: "ip(2) arg 1 (2) typeError got INT64, expected STRING",
		AstErr:     "input to 'ip' func was not a string",
		conf:       exprEvalAttrs,
	},
	{
		E:      `destination.ip| ip("10.1.12")`,
		Type:   descriptor.IP_ADDRESS,
		I:      map[string]interface{}{},
		Err:    "could not convert 10.1.12 to IP_ADDRESS",
		AstErr: "could not convert '10.1.12' to IP_ADDRESS",
		conf:   exprEvalAttrs,
	},
	{
		E:          `request.time | timestamp("2015-01-02T15:04:35Z")`,
		Type:       descriptor.TIMESTAMP,
		I:          map[string]interface{}{},
		R:          t,
		Referenced: []string{"-request.time"},
		conf:       exprEvalAttrs,
	},
	{
		E:    `request.time | timestamp(2)`,
		Type: descriptor.TIMESTAMP,
		I: map[string]interface{}{
			"request.time": "",
		},
		CompileErr: "timestamp(2) arg 1 (2) typeError got INT64, expected STRING",
		AstErr:     "input to 'timestamp' func was not a string",
		conf:       exprEvalAttrs,
	},
	{
		E:      `request.time | timestamp("242233")`,
		Type:   descriptor.TIMESTAMP,
		I:      map[string]interface{}{},
		Err:    "could not convert '242233' to TIMESTAMP. expected format: '" + time.RFC3339 + "'",
		AstErr: "could not convert '242233' to TIMESTAMP. expected format: '" + time.RFC3339 + "'",
		conf:   exprEvalAttrs,
	},
	{
		E:    "emptyStringMap()",
		Type: descriptor.STRING_MAP,
		IL: `
fn eval() interface
  call emptyStringMap
  ret
end
`,
		R:          attribute.WrapStringMap(nil),
		I:          map[string]interface{}{},
		Referenced: []string{},
		conf:       exprEvalAttrs,
	},
	{
		E:          `source.labels | emptyStringMap()`,
		Type:       descriptor.STRING_MAP,
		I:          map[string]interface{}{},
		R:          attribute.WrapStringMap(nil),
		Referenced: []string{"-source.labels"},
		conf:       exprEvalAttrs,
	},

	{
		E:          `emptyStringMap() | source.labels`,
		Type:       descriptor.STRING_MAP,
		I:          map[string]interface{}{"source.labels": attribute.WrapStringMap(map[string]string{"test": "foo"})},
		R:          attribute.WrapStringMap(nil),
		Referenced: []string{},
		conf:       exprEvalAttrs,
	},

	// Tests from expr/eval_test.go TestCEXLEval
	{
		E: "a = 2",
		I: map[string]interface{}{
			"a": int64(2),
		},
		CompileErr: "unable to parse expression 'a = 2': 1:3: expected '==', found '='",
		AstErr:     "unable to parse",
		conf:       exprEvalAttrs,
	},
	{
		E:    "a == 2",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:    true,
		conf: exprEvalAttrs,
	},
	{
		E:    "a == 3",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:    false,
		conf: exprEvalAttrs,
	},
	{
		E:    "a == 2",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"a": int64(2),
		},
		R:    true,
		conf: exprEvalAttrs,
	},
	{
		E:    "a == 2",
		Type: descriptor.BOOL,
		I:    map[string]interface{}{},
		Err:  "lookup failed: 'a'",
		// CEL always resolves attributes
		CEL:    false,
		AstErr: "unresolved attribute",
		conf:   exprEvalAttrs,
	},
	{
		E:    `request.user | "user1"`,
		Type: descriptor.STRING,
		I:    map[string]interface{}{},
		R:    "user1",
		conf: exprEvalAttrs,
	},
	{
		E:    "a == 2",
		Type: descriptor.BOOL,
		I:    map[string]interface{}{},
		Err:  "lookup failed: 'a'",
		// CEL always resolves attributes
		CEL:    false,
		AstErr: "unresolved attribute",
		conf:   exprEvalAttrs,
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
  jz L0
  apush_b true
  jmp L1
L0:
  apush_b false
L1:
  ret
end`,
	},
	{
		E:     `true && false`,
		Bench: true,
		Type:  descriptor.BOOL,
		R:     false,
		IL: `
fn eval() bool
  apush_b true
  jz L0
  apush_b false
  jmp L1
L0:
  apush_b false
L1:
  ret
end`,
	},
	{
		E:     `true && true`,
		Bench: true,
		Type:  descriptor.BOOL,
		R:     true,
		IL: `
fn eval() bool
  apush_b true
  jz L0
  apush_b true
  jmp L1
L0:
  apush_b false
L1:
  ret
end `,
	},
	{
		E:     `false && false`,
		Bench: true,
		Type:  descriptor.BOOL,
		R:     false,
		IL: `
fn eval() bool
  apush_b false
  jz L0
  apush_b false
  jmp L1
L0:
  apush_b false
L1:
  ret
end`,
	},
	{
		E:    `false && false && false`,
		Type: descriptor.BOOL,
		R:    false,
	},
	{
		E:    `false && false && true`,
		Type: descriptor.BOOL,
		R:    false,
	},
	{
		E:    `false && true && false`,
		Type: descriptor.BOOL,
		R:    false,
	},
	{
		E:    `true && false && false`,
		Type: descriptor.BOOL,
		R:    false,
	},
	{
		E:    `true && true && false`,
		Type: descriptor.BOOL,
		R:    false,
	},
	{
		E:    `true && false && true`,
		Type: descriptor.BOOL,
		R:    false,
	},
	{
		E:    `false && true && true`,
		Type: descriptor.BOOL,
		R:    false,
	},
	{
		E:    `true && true && true`,
		Type: descriptor.BOOL,
		R:    true,
	},
	{
		E:    "b1 && b2",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"b1": false,
			"b2": false,
		},
		R:          false,
		Referenced: []string{"b1"},
	},
	{
		E:    "b1 && b2",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"b1": false,
			"b2": true,
		},
		R:          false,
		Referenced: []string{"b1"},
	},
	{
		E:    "b1 && b2",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"b1": true,
			"b2": false,
		},
		R:          false,
		Referenced: []string{"b1", "b2"},
	},
	{
		E:    "b1 && b2",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"b1": true,
			"b2": true,
		},
		R:          true,
		Referenced: []string{"b1", "b2"},
	},
	{
		E:    "b1 || b2",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"b1": false,
			"b2": false,
		},
		R:          false,
		Referenced: []string{"b1", "b2"},
	},
	{
		E:    "b1 || b2",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"b1": false,
			"b2": true,
		},
		R:          true,
		Referenced: []string{"b1", "b2"},
	},
	{
		E:    "b1 || b2",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"b1": true,
			"b2": false,
		},
		R:          true,
		Referenced: []string{"b1"},
	},
	{
		E:    "b1 || b2",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"b1": true,
			"b2": true,
		},
		R:          true,
		Referenced: []string{"b1"},
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
		E:    `dnsName("foo.bar.baz")`,
		Type: descriptor.DNS_NAME,
		R:    "foo.bar.baz",
		IL: `
fn eval() string
  apush_s "foo.bar.baz"
  call dnsName
  ret
end`,
	},

	{
		E:    `adns`,
		Type: descriptor.DNS_NAME,
		I: map[string]interface{}{
			"adns": "foo.bar",
		},
		R: "foo.bar",
		IL: `
fn eval() string
  resolve_s "adns"
  ret
end
`,
	},

	{
		E:    `dnsName("")`,
		Type: descriptor.DNS_NAME,
		Err:  `error converting '' to dns name: 'idna: invalid label ""'`,
	},

	{
		E:    `dnsName(abc)`,
		Type: descriptor.DNS_NAME,
		Err:  "lookup failed: 'abc'",
	},

	{
		E:    `dnsName(abc)`,
		Type: descriptor.DNS_NAME,
		I: map[string]interface{}{
			"abc": "-foo.-bar",
		},
		Err: `error converting '-foo.-bar' to dns name: 'idna: invalid label "-foo"'`,
	},

	{
		E:    `dnsName(abc)`,
		Type: descriptor.DNS_NAME,
		I: map[string]interface{}{
			"abc": "foo.bar",
		},
		R: "foo.bar",
		IL: `
fn eval() string
  resolve_s "abc"
  call dnsName
  ret
end
`,
	},

	{
		E:    `adns | dnsName("foo.bar.baz")`,
		Type: descriptor.DNS_NAME,
		I:    map[string]interface{}{},
		R:    "foo.bar.baz",
		// top-level idents do not support presence
		CEL: "",
		IL: `
fn eval() string
  tresolve_s "adns"
  jnz L0
  apush_s "foo.bar.baz"
  call dnsName
L0:
  ret
end`,
	},

	{
		E:    `adns | bdns | dnsName("foo.bar.baz")`,
		Type: descriptor.DNS_NAME,
		I:    map[string]interface{}{},
		R:    "foo.bar.baz",
		// top-level idents do not support presence
		CEL: "",
		IL: `
fn eval() string
  tresolve_s "adns"
  jnz L0
  tresolve_s "bdns"
  jnz L0
  apush_s "foo.bar.baz"
  call dnsName
L0:
  ret
end
`,
	},

	{
		E:    `adns | dnsName("foo.bar.baz") | bdns`,
		Type: descriptor.DNS_NAME,
		I:    map[string]interface{}{},
		R:    "foo.bar.baz",
		// top-level idents do not support presence
		CEL: "",
		IL: `
fn eval() string
  tresolve_s "adns"
  jnz L0
  apush_s "foo.bar.baz"
  call dnsName
  jmp L0
  resolve_s "bdns"
L0:
  ret
end
`,
	},

	{
		E:    `adns | dnsName("foo.bar.baz")`,
		Type: descriptor.DNS_NAME,
		I: map[string]interface{}{
			"adns": "www.istio.io",
		},
		R: "www.istio.io",
	},

	{
		E:    `adns == bdns`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `
fn eval() bool
  resolve_s "adns"
  resolve_s "bdns"
  call dnsName_equal
  ret
end`,
		I: map[string]interface{}{
			"adns": "foo.bar.com",
			"bdns": "fOO.bar.com",
		},
	},

	{
		E:    `dnsName(abc | bs | "foo")`,
		Type: descriptor.DNS_NAME,
		R:    "foo",
		// CEL does not support top-level ident presence
		CEL: errors.New("error converting"),
		IL: `
 fn eval() string
  tresolve_s "abc"
  jnz L0
  tresolve_s "bs"
  jnz L0
  apush_s "foo"
L0:
  call dnsName
  ret
end`,
	},

	{
		E:    `dnsName(abc | bs | "foo")`,
		Type: descriptor.DNS_NAME,
		I: map[string]interface{}{
			"abc": "foo.bar.com",
		},
		R: "foo.bar.com",
	},

	{
		E:    `adns == bdns`,
		Type: descriptor.BOOL,
		R:    false,
		I: map[string]interface{}{
			"adns": "foo.bar.com",
			"bdns": "bar.foo.com",
		},
	},

	{
		E:    `adns != bdns`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `
fn eval() bool
  resolve_s "adns"
  resolve_s "bdns"
  call dnsName_equal
  not
  ret
end`,
		I: map[string]interface{}{
			"adns": "foo.bar.com",
			"bdns": "bar.foo.com",
		},
	},

	{
		E:    `adns != bdns`,
		Type: descriptor.BOOL,
		R:    false,
		I: map[string]interface{}{
			"adns": "foo.bar.com",
			"bdns": "foo.bar.com",
		},
	},

	{
		E:          `adns == abc`,
		CompileErr: "EQ($adns, $abc) arg 2 ($abc) typeError got STRING, expected DNS_NAME",
	},

	{
		E:          `adns != abc`,
		CompileErr: "NEQ($adns, $abc) arg 2 ($abc) typeError got STRING, expected DNS_NAME",
	},

	{
		E:    `dnsName("foo.bar.baz") == dnsName("foo.Bar.baz.")`,
		Type: descriptor.BOOL,
		R:    true,
	},

	{
		E:    `(adns | dnsName("foo.bar.baz")) == dnsName("foo.Bar.baz.")`,
		Type: descriptor.BOOL,
		R:    true,
		// CEL does not have top-level ident presence
		CEL: false,
	},

	{
		E:    `(adns | dnsName("foo.bar.baz")) == dnsName("foo.Bar.baz.")`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"adns": "foo.bar.com",
		},
		R: false,
	},

	{
		E:    `email("istio@istio.io")`,
		Type: descriptor.EMAIL_ADDRESS,
		R:    "istio@istio.io",
		IL: `
fn eval() string
  apush_s "istio@istio.io"
  call email
  ret
end`,
	},

	{
		E:    `amail`,
		Type: descriptor.EMAIL_ADDRESS,
		I: map[string]interface{}{
			"amail": "foo@bar.com",
		},
		R: "foo@bar.com",
		IL: `
fn eval() string
  resolve_s "amail"
  ret
end
`,
	},

	{
		E:    `email("")`,
		Type: descriptor.EMAIL_ADDRESS,
		Err:  `error converting '' to e-mail: 'mail: no address'`,
	},

	{
		E:    `email(abc)`,
		Type: descriptor.EMAIL_ADDRESS,
		Err:  "lookup failed: 'abc'",
	},

	{
		E:    `email(abc)`,
		Type: descriptor.EMAIL_ADDRESS,
		I: map[string]interface{}{
			"abc": "barfoo",
		},
		Err: `error converting 'barfoo' to e-mail:`,
	},

	{
		E:    `email(abc)`,
		Type: descriptor.EMAIL_ADDRESS,
		I: map[string]interface{}{
			"abc": "istio@istio.io",
		},
		R: "istio@istio.io",
		IL: `
fn eval() string
  resolve_s "abc"
  call email
  ret
end
`,
	},

	{
		E:    `email(abc)`,
		Type: descriptor.EMAIL_ADDRESS,
		I: map[string]interface{}{
			"abc": `"istio"@istio.io`, // The e-mail should not get normalized.
		},
		R: `"istio"@istio.io`,
	},

	{
		E:    `amail | email("istio@istio.io")`,
		Type: descriptor.EMAIL_ADDRESS,
		I:    map[string]interface{}{},
		R:    "istio@istio.io",
		// top-level idents do not support presence
		CEL: "",
		IL: `
fn eval() string
  tresolve_s "amail"
  jnz L0
  apush_s "istio@istio.io"
  call email
L0:
  ret
end`,
	},

	{
		E:    `amail | bmail | email("istio@istio.io")`,
		Type: descriptor.EMAIL_ADDRESS,
		I:    map[string]interface{}{},
		R:    "istio@istio.io",
		// top-level idents do not support presence
		CEL: "",
		IL: `
fn eval() string
  tresolve_s "amail"
  jnz L0
  tresolve_s "bmail"
  jnz L0
  apush_s "istio@istio.io"
  call email
L0:
  ret
end
`,
	},

	{
		E:    `amail | email("istio@istio.io") | bmail`,
		Type: descriptor.EMAIL_ADDRESS,
		I:    map[string]interface{}{},
		R:    "istio@istio.io",
		// top-level idents do not support presence
		CEL: "",
		IL: `
fn eval() string
  tresolve_s "amail"
  jnz L0
  apush_s "istio@istio.io"
  call email
  jmp L0
  resolve_s "bmail"
L0:
  ret
end
`,
	},

	{
		E:    `amail | email("kubernetes@kubernetes.io")`,
		Type: descriptor.EMAIL_ADDRESS,
		I: map[string]interface{}{
			"amail": "istio@istio.io",
		},
		R: "istio@istio.io",
	},

	{
		E:    `amail == bmail`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `
fn eval() bool
  resolve_s "amail"
  resolve_s "bmail"
  call email_equal
  ret
end`,
		I: map[string]interface{}{
			"amail": `"istio"@istio.io`,
			"bmail": "istio@istio.io",
		},
	},

	{
		E:    `email(abc | bs | "istio@istio.io")`,
		Type: descriptor.EMAIL_ADDRESS,
		R:    "istio@istio.io",
		// CEL does not support top-level ident presence
		CEL: errors.New("error converting"),
		IL: `
 fn eval() string
  tresolve_s "abc"
  jnz L0
  tresolve_s "bs"
  jnz L0
  apush_s "istio@istio.io"
L0:
  call email
  ret
end`,
	},

	{
		E:    `email(abc | bs | "pilot@istio.io")`,
		Type: descriptor.EMAIL_ADDRESS,
		I: map[string]interface{}{
			"abc": "istio@istio.io",
		},
		R: "istio@istio.io",
	},

	{
		E:    `amail == bmail`,
		Type: descriptor.BOOL,
		R:    false,
		I: map[string]interface{}{
			"amail": "istio@istio.io",
			"bmail": "pilot@istio.io",
		},
	},

	{
		E:    `amail != bmail`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `
fn eval() bool
  resolve_s "amail"
  resolve_s "bmail"
  call email_equal
  not
  ret
end`,
		I: map[string]interface{}{
			"amail": "istio@istio.io",
			"bmail": "pilot@istio.io",
		},
	},

	{
		E:    `amail != bmail`,
		Type: descriptor.BOOL,
		R:    false,
		I: map[string]interface{}{
			"amail": "istio@istio.io",
			"bmail": "istio@istio.io",
		},
	},

	{
		E:          `amail == abc`,
		CompileErr: "EQ($amail, $abc) arg 2 ($abc) typeError got STRING, expected EMAIL_ADDRESS",
	},

	{
		E:          `amail != abc`,
		CompileErr: "NEQ($amail, $abc) arg 2 ($abc) typeError got STRING, expected EMAIL_ADDRESS",
	},

	{
		E:    `email("istio@istio.io") == email("istio@istio.io")`,
		Type: descriptor.BOOL,
		R:    true,
	},

	{
		E:    `uri("http://istio.io")`,
		Type: descriptor.URI,
		R:    "http://istio.io",
		IL: `
fn eval() string
  apush_s "http://istio.io"
  call uri
  ret
end`,
	},

	{
		E:    `auri`,
		Type: descriptor.URI,
		I: map[string]interface{}{
			"auri": "http://istio.io",
		},
		R: "http://istio.io",
		IL: `
fn eval() string
  resolve_s "auri"
  ret
end
`,
	},

	{
		E:    `uri("")`,
		Type: descriptor.URI,
		Err:  `error converting string to uri: empty string`,
	},

	{
		E:    `uri(abc)`,
		Type: descriptor.URI,
		Err:  "lookup failed: 'abc'",
	},

	{
		E:    `uri(abc)`,
		Type: descriptor.URI,
		I: map[string]interface{}{
			"abc": ":/",
		},
		Err: `error converting string to uri`,
	},

	{
		E:    `uri(abc)`,
		Type: descriptor.URI,
		I: map[string]interface{}{
			"abc": "urn:foo",
		},
		R: "urn:foo",
		IL: `
fn eval() string
  resolve_s "abc"
  call uri
  ret
end
`,
	},

	{
		E:    `auri | uri("urn:foo")`,
		Type: descriptor.URI,
		I:    map[string]interface{}{},
		R:    "urn:foo",
		// top-level idents do not support presence
		CEL: "",
		IL: `
fn eval() string
  tresolve_s "auri"
  jnz L0
  apush_s "urn:foo"
  call uri
L0:
  ret
end`,
	},

	{
		E:    `auri | buri | uri("https://kubernetes.io")`,
		Type: descriptor.URI,
		I:    map[string]interface{}{},
		R:    "https://kubernetes.io",
		// top-level idents do not support presence
		CEL: "",
		IL: `
fn eval() string
  tresolve_s "auri"
  jnz L0
  tresolve_s "buri"
  jnz L0
  apush_s "https://kubernetes.io"
  call uri
L0:
  ret
end
`,
	},

	{
		E:    `auri | uri("https://kubernetes.io") | buri`,
		Type: descriptor.URI,
		I:    map[string]interface{}{},
		R:    "https://kubernetes.io",
		// top-level idents do not support presence
		CEL: "",
		IL: `
fn eval() string
  tresolve_s "auri"
  jnz L0
  apush_s "https://kubernetes.io"
  call uri
  jmp L0
  resolve_s "buri"
L0:
  ret
end
`,
	},

	{
		E:    `auri | uri("https://kubernetes.io")`,
		Type: descriptor.URI,
		I: map[string]interface{}{
			"auri": "www.istio.io",
		},
		R: "www.istio.io",
	},

	{
		E:    `auri == buri`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `
fn eval() bool
  resolve_s "auri"
  resolve_s "buri"
  call uri_equal
  ret
end`,
		I: map[string]interface{}{
			"auri": "http://foo.bar.com",
			"buri": "http://fOO.bar.com",
		},
	},

	{
		E:    `uri(abc | bs | "ftp://ftp.istio.io/releases")`,
		Type: descriptor.URI,
		R:    "ftp://ftp.istio.io/releases",
		// CEL does not support top-level ident presence
		CEL: errors.New("error converting"),
		IL: `
 fn eval() string
  tresolve_s "abc"
  jnz L0
  tresolve_s "bs"
  jnz L0
  apush_s "ftp://ftp.istio.io/releases"
L0:
  call uri
  ret
end`,
	},

	{
		E:    `uri(abc | bs | "ftp://ftp.istio.io/releases")`,
		Type: descriptor.URI,
		I: map[string]interface{}{
			"abc": "http://istio.io",
		},
		R: "http://istio.io",
	},

	{
		E:    `auri == buri`,
		Type: descriptor.BOOL,
		R:    false,
		I: map[string]interface{}{
			"auri": "http://istio.io:80",
			"buri": "http://istio.io:81",
		},
	},

	{
		E:    `auri != buri`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `
fn eval() bool
  resolve_s "auri"
  resolve_s "buri"
  call uri_equal
  not
  ret
end`,
		I: map[string]interface{}{
			"auri": "http://istio.io:80",
			"buri": "http://istio.io:81",
		},
	},

	{
		E:    `auri != buri`,
		Type: descriptor.BOOL,
		R:    false,
		I: map[string]interface{}{
			"auri": "http://istio.io:80",
			"buri": "http://istio.io:80",
		},
	},

	{
		E:          `auri == abc`,
		CompileErr: "EQ($auri, $abc) arg 2 ($abc) typeError got STRING, expected URI",
	},

	{
		E:          `auri != abc`,
		CompileErr: "NEQ($auri, $abc) arg 2 ($abc) typeError got STRING, expected URI",
	},

	{
		E:    `uri("http://foo.bar.baz") == uri("http://foo.Bar.baz.")`,
		Type: descriptor.BOOL,
		R:    true,
	},

	{
		E:    `(auri | uri("http://foo.bar.baz")) == uri("http://foo.Bar.baz.")`,
		Type: descriptor.BOOL,
		R:    true,
		// top-level idents do not support presence
		CEL: false,
	},

	{
		E:    `(auri | uri("https://foo.bar.baz")) == uri("https://foo.Bar.baz.")`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"auri": "foo.bar.com",
		},
		R: false,
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
		// top-level idents do not support presence
		CEL: false,
	},
	{
		E:    `abc`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"abc": "AAA",
		},
		R: "AAA",
		IL: `
fn eval() string
  resolve_s "abc"
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
		R:          "c",
		Referenced: []string{"ar", "ar[b]"},
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
		E:       `abc | ""`,
		Type:    descriptor.STRING,
		R:       "",
		Skipast: true, // ast returns nil for this cabce.
	},
	{
		E:    `abc | ""`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"abc": "foo",
		},
		R: "foo",
	},
	{
		E:    `abc | "user1"`,
		Type: descriptor.STRING,
		R:    "user1",
		// top-level idents do not support presence
		CEL: "",
		IL: `
fn eval() string
  tresolve_s "abc"
  jnz L0
  apush_s "user1"
L0:
  ret
end`,
	},
	{
		E:    `abc | "user1"`,
		Type: descriptor.STRING,
		R:    "a2",
		I: map[string]interface{}{
			"abc": "a2",
		},
	},
	{
		E:    `abc | bs | "user1"`,
		Type: descriptor.STRING,
		R:    "user1",
		// top-level idents do not support presence
		CEL: "",
		IL: `
fn eval() string
  tresolve_s "abc"
  jnz L0
  tresolve_s "bs"
  jnz L0
  apush_s "user1"
L0:
  ret
end`,
	},
	{
		E:    `abc | bs | "user1"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"abc": "a2",
		},
		R:          "a2",
		Referenced: []string{"abc"},
	},
	{
		E:    `abc | bs | "user1"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"bs": "b2",
		},
		R: "b2",
		// top-level idents do not support presence
		CEL:           "",
		Referenced:    []string{"-abc", "bs"},
		ReferencedCEL: []string{"-abc"},
	},
	{
		E:    `abc | bs | "user1"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"abc": "a2",
			"bs":  "b2",
		},
		R:          "a2",
		Referenced: []string{"abc"},
	},

	{
		E:    `ab | true`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"ab": false,
		},
		R:          false,
		Referenced: []string{"ab"},
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
		// top-level idents do not support presence
		CEL: false,
	},
	{
		E:    `ab | bb | true`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"ab": false,
		},
		R:          false,
		Referenced: []string{"ab"},
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
		R:             false,
		Referenced:    []string{"-ab", "bb"},
		ReferencedCEL: []string{"-ab"},
	},
	{
		E:    `ab | bb | true`,
		Type: descriptor.BOOL,
		I:    map[string]interface{}{},
		R:    true,
		// top-level idents do not support presence
		CEL:           false,
		Referenced:    []string{"-ab", "-bb"},
		ReferencedCEL: []string{"-ab"},
	},

	{
		E:    `ai | 42`,
		Type: descriptor.INT64,
		I: map[string]interface{}{
			"ai": int64(10),
		},
		R:          int64(10),
		Referenced: []string{"ai"},
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
		// top-level idents do not support presence
		CEL:        int64(0),
		Referenced: []string{"-ai"},
	},
	{
		E:    `ai | bi | 42`,
		Type: descriptor.INT64,
		I: map[string]interface{}{
			"ai": int64(10),
		},
		R:          int64(10),
		Referenced: []string{"ai"},
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
		// top-level idents do not support presence
		CEL:           int64(0),
		Referenced:    []string{"-ai", "bi"},
		ReferencedCEL: []string{"-ai"},
	},

	{
		E:    `ai | bi | 42`,
		Type: descriptor.INT64,
		I:    map[string]interface{}{},
		R:    int64(42),
		// top-level idents do not support presence
		CEL:           int64(0),
		Referenced:    []string{"-ai", "-bi"},
		ReferencedCEL: []string{"-ai"},
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
		// top-level idents do not support presence
		CEL: float64(0),
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
		// top-level idents do not support presence
		CEL: float64(0),
	},
	{
		E:    `ad | bd | 42.1`,
		Type: descriptor.DOUBLE,
		I:    map[string]interface{}{},
		R:    float64(42.1),
		// top-level idents do not support presence
		CEL: float64(0),
	},

	{
		E:       `(ar | br)["foo"]`,
		Type:    descriptor.STRING,
		Skipast: true, // ast evaluator panics for this cabce.
		I: map[string]interface{}{
			"ar": map[string]string{
				"foo": "bar",
			},
			"br": map[string]string{
				"foo": "far",
			},
		},
		R:          "bar",
		Referenced: []string{"ar", "ar[foo]"},
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
		Skipast: true, // ast evaluator panics for this cabce.
		I: map[string]interface{}{
			"br": map[string]string{
				"foo": "far",
			},
		},
		R: "far",
		// CEL does not support top-level ident presence
		CEL:        errors.New("no such key"),
		Referenced: []string{"-ar", "br", "br[foo]"},
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
		E:    "abc == bs",
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"abc": "ABC",
			"bs":  "ABC",
		},
		R: true,
		IL: `
fn eval() bool
  resolve_s "abc"
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
		R:          "bar",
		Referenced: []string{"sm", "sm[foo]"},
		IL: `
fn eval() string
  resolve_f "sm"
  anlookup "foo"
  ret
end`,
	},
	{
		E:    `sm[abc]`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"abc": "foo",
			"sm":  map[string]string{"foo": "bar"},
		},
		R:          "bar",
		Referenced: []string{"abc", "sm", "sm[foo]"},
		IL: `
fn eval() string
  resolve_f "sm"
  resolve_s "abc"
  nlookup
  ret
end`,
	},
	{
		E:          `ar["c"] | "foo"`,
		Type:       descriptor.STRING,
		I:          map[string]interface{}{},
		R:          "foo",
		Referenced: []string{"-ar"},
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
		E:          `ar[abc] | "foo"`,
		Type:       descriptor.STRING,
		I:          map[string]interface{}{},
		R:          "foo",
		Referenced: []string{"-ar"},
		// CEL index operator is a function that always resolves the index
		ReferencedCEL: []string{"-abc", "-ar"},
		IL: `
fn eval() string
  tresolve_f "ar"
  jnz L0
  jmp L1
L0:
  tresolve_s "abc"
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
		E:    `ar[abc] | "foo"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"ar": map[string]string{"abc": "bar"},
		},
		R:          "foo",
		Referenced: []string{"-abc", "ar"},
		// CEL resolved missing stringmap attributes to an empty map
		ReferencedCEL: []string{"-abc", "-ar[]", "ar"},
	},
	{
		E:    `ar[abc] | "foo"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"abc": "bar",
		},
		R:          "foo",
		Referenced: []string{"-ar"},
		// CEL index operator is a function that always resolves the index
		ReferencedCEL: []string{"-ar", "abc"},
	},
	{
		E:    `ar[abc] | "foo"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"ar":  map[string]string{"abc": "bar"},
			"abc": "!!!!",
		},
		R:          "foo",
		Referenced: []string{"-ar[!!!!]", "abc", "ar"},
	},
	{
		E:    `ar[abc] | "foo"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"ar":  map[string]string{"abcval": "bar"},
			"abc": "abcval",
		},
		R:          "bar",
		Referenced: []string{"abc", "ar", "ar[abcval]"},
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
		R:          "c",
		Referenced: []string{"ar", "ar[b]"},
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
		R:          "null",
		Referenced: []string{"-ar[b]", "-ar[c]", "ar"},
	},
	{
		E:    `ar["b"] | ar["c"] | "null"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"ar": map[string]string{
				"b": "c",
			},
		},
		R:          "c",
		Referenced: []string{"ar", "ar[b]"},
	},
	{
		E:    `ar["b"] | ar["c"] | "null"`,
		Type: descriptor.STRING,
		I: map[string]interface{}{
			"ar": map[string]string{
				"c": "b",
			},
		},
		R:          "b",
		Referenced: []string{"-ar[b]", "ar", "ar[c]"},
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
		// top-level idents do not support presence
		CEL: time.Duration(0),
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
		// CEL does not support top-level ident presence
		CEL: time.Unix(0, 0).UTC(),
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
		// top-level idents do not support presence
		CEL: []byte{},
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
		E:    `timestamp("2015-01-02T15:04:35Z") < timestamp("2015-01-02T15:04:36Z")`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `fn eval() bool
  apush_s "2015-01-02T15:04:35Z"
  call timestamp
  apush_s "2015-01-02T15:04:36Z"
  call timestamp
  call timestamp_lt
  ret
end`,
	},
	{
		E:    `t1 < timestamp("2015-01-02T15:04:36Z")`,
		Type: descriptor.BOOL,
		R:    true,
		I: map[string]interface{}{
			"t1": t,
		},
		IL: `fn eval() bool
  resolve_f "t1"
  apush_s "2015-01-02T15:04:36Z"
  call timestamp
  call timestamp_lt
  ret
end`,
	},
	{
		E:    `t1 <= t2`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"t1": t,
			"t2": t2,
		},
		R: false,
		IL: `fn eval() bool
  resolve_f "t1"
  resolve_f "t2"
  call timestamp_le
  ret
end`,
	},
	{
		E:    `t2 <= t1`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"t1": t,
			"t2": t2,
		},
		R: true,
		IL: `fn eval() bool
  resolve_f "t2"
  resolve_f "t1"
  call timestamp_le
  ret
end`,
	},
	{
		E:          `t1 <= 42`,
		CompileErr: "LEQ($t1, 42) arg 2 (42) typeError got INT64, expected TIMESTAMP",
	},
	{
		E:          `t1 < 42`,
		CompileErr: "LT($t1, 42) arg 2 (42) typeError got INT64, expected TIMESTAMP",
	},
	{
		E:          `42 <= t1`,
		CompileErr: "LEQ(42, $t1) arg 2 ($t1) typeError got TIMESTAMP, expected INT64",
	},
	{
		E:    `t1 <= t1`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"t1": t,
		},
		R: true,
		IL: `fn eval() bool
  resolve_f "t1"
  resolve_f "t1"
  call timestamp_le
  ret
end`,
	},
	{
		E:    `t1 < t1`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"t1": t,
		},
		R: false,
		IL: `fn eval() bool
  resolve_f "t1"
  resolve_f "t1"
  call timestamp_lt
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
		E:    `t1 > t2`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"t1": t,
			"t2": t2,
		},
		R: true,
		IL: `fn eval() bool
  resolve_f "t1"
  resolve_f "t2"
  call timestamp_gt
  ret
end`,
	},
	{
		E:    `t1 >= t2`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"t1": t,
			"t2": t2,
		},
		R: true,
		IL: `fn eval() bool
  resolve_f "t1"
  resolve_f "t2"
  call timestamp_ge
  ret
end`,
	},
	{
		E:    `t1 > t1`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"t1": t,
		},
		R: false,
		IL: `fn eval() bool
  resolve_f "t1"
  resolve_f "t1"
  call timestamp_gt
  ret
end`,
	},
	{
		E:    `t1 >= t1`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"t1": t,
		},
		R: true,
		IL: `fn eval() bool
  resolve_f "t1"
  resolve_f "t1"
  call timestamp_ge
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
		// CEL does not support top-level ident presence
		CEL: time.Unix(0, 0).UTC(),
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
		R: []byte(net.ParseIP("1.2.3.4")),
	},

	{
		E:    `ip(abc)`,
		Type: descriptor.IP_ADDRESS,
		I: map[string]interface{}{
			"abc": "1.2.3.4",
		},
		IL: `
fn eval() interface
  resolve_s "abc"
  call ip
  ret
end
		`,
		R: []byte(net.ParseIP("1.2.3.4")),
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
		R: []byte(net.ParseIP("1.2.3.4")),
	},

	{
		E:    `ip(abc | "5.6.7.8")`,
		Type: descriptor.IP_ADDRESS,
		IL: `
fn eval() interface
  tresolve_s "abc"
  jnz L0
  apush_s "5.6.7.8"
L0:
  call ip
  ret
end
`,
		R: []byte(net.ParseIP("5.6.7.8")),
		// CEL does not support top-level ident presence
		CEL: errors.New("could not convert"),
	},

	{
		E:    `ip(abc | bs)`,
		Type: descriptor.IP_ADDRESS,
		I: map[string]interface{}{
			"bs": "1.2.3.4",
		},
		R: []byte(net.ParseIP("1.2.3.4")),
		// CEL does not support top-level ident presence
		CEL: errors.New("could not convert"),
	},

	{
		E:    `ip(abc | bs)`,
		Type: descriptor.IP_ADDRESS,
		Err:  "lookup failed: 'bs'",
		// CEL does not support top-level ident presence
		CEL: errors.New("could not convert"),
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
		R: []byte(net.ParseIP("1.2.3.4")),
	},

	{
		E:    `ip(abc) | ip(bs)`,
		Type: descriptor.IP_ADDRESS,
		I: map[string]interface{}{
			"abc": "1.2.3.4",
			"bs":  "5.6.7.8",
		},
		IL: `
fn eval() interface
  resolve_s "abc"
  call ip
  jmp L0
  resolve_s "bs"
  call ip
L0:
  ret
end
`,
		R: []byte(net.ParseIP("1.2.3.4")),
	},

	{
		E:    `ip(abc) | ip(bs)`,
		Type: descriptor.IP_ADDRESS,
		I: map[string]interface{}{
			"bs": "1.2.3.4",
		},
		Err: `lookup failed: 'abc'`,
	},

	{
		E:    `true | "foo".startsWith("bar")`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `
fn eval() bool
  apush_b true
  jmp L0
  apush_s "foo"
  apush_s "bar"
  call startsWith
L0:
  ret
end`,
	},

	{
		E:    `"foo".startsWith("bar") | true`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `
fn eval() bool
  apush_s "foo"
  apush_s "bar"
  call startsWith
  jmp L0
  apush_b true
L0:
  ret
end`,
	},

	{
		E:    `"foo".startsWith("bar") | ab`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"ab": true,
		},
		R: false,
		IL: `
fn eval() bool
  apush_s "foo"
  apush_s "bar"
  call startsWith
  jmp L0
  resolve_b "ab"
L0:
  ret
end`,
	},

	{
		E:    `"foo".startsWith("f") | ab`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"ab": false,
		},
		R: true,
		IL: `
fn eval() bool
  apush_s "foo"
  apush_s "f"
  call startsWith
  jmp L0
  resolve_b "ab"
L0:
  ret
end`,
	},

	{
		E:    `ab | "foo".startsWith("f")`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"ab": false,
		},
		R: false,
		IL: `
fn eval() bool
  tresolve_b "ab"
  jnz L0
  apush_s "foo"
  apush_s "f"
  call startsWith
L0:
  ret
end`,
	},

	{
		E:    `ab | "foo".startsWith("f")`,
		Type: descriptor.BOOL,
		R:    true,
		// CEL does not support top-level ident presence
		CEL: false,
		IL: `
fn eval() bool
  tresolve_b "ab"
  jnz L0
  apush_s "foo"
  apush_s "f"
  call startsWith
L0:
  ret
end`,
	},

	{
		E:    `"foo".startsWith("bar") | ab | "foo".startsWith("f")`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `
fn eval() bool
  apush_s "foo"
  apush_s "bar"
  call startsWith
  jmp L0
  tresolve_b "ab"
  jnz L0
  apush_s "foo"
  apush_s "f"
  call startsWith
L0:
  ret
end
`,
	},

	{
		E:    `ab | "foo".startsWith("bar") | "foo".startsWith("f")`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `
fn eval() bool
  tresolve_b "ab"
  jnz L0
  apush_s "foo"
  apush_s "bar"
  call startsWith
  jmp L0
  apush_s "foo"
  apush_s "f"
  call startsWith
L0:
  ret
end`,
	},

	{
		E:    `ab | "foo".startsWith("bar") | bb`,
		Type: descriptor.BOOL,
		R:    false,
		I: map[string]interface{}{
			"bb": true,
		},
		IL: `
fn eval() bool
  tresolve_b "ab"
  jnz L0
  apush_s "foo"
  apush_s "bar"
  call startsWith
  jmp L0
  resolve_b "bb"
L0:
  ret
end`,
	},

	{
		E:    `ab | bb | "foo".startsWith("bar")`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `
fn eval() bool
  tresolve_b "ab"
  jnz L0
  tresolve_b "bb"
  jnz L0
  apush_s "foo"
  apush_s "bar"
  call startsWith
L0:
  ret
end
`,
	},

	{
		E:    `ab | bb | "foo".startsWith("bar")`,
		Type: descriptor.BOOL,
		R:    true,
		// CEL does not support top-level ident presence
		CEL: false,
		I: map[string]interface{}{
			"bb": true,
		},
	},
	{
		E:    `ab | true | "foo".startsWith("bar")`,
		Type: descriptor.BOOL,
		R:    true,
		// CEL does not support top-level ident presence
		CEL: false,
		IL: `
fn eval() bool
  tresolve_b "ab"
  jnz L0
  apush_b true
  jmp L0
  apush_s "foo"
  apush_s "bar"
  call startsWith
L0:
  ret
end
`,
	},

	{
		E:    `ab | "foo".startsWith("bar") | true`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `
fn eval() bool
  tresolve_b "ab"
  jnz L0
  apush_s "foo"
  apush_s "bar"
  call startsWith
  jmp L0
  apush_b true
L0:
  ret
end
`,
	},

	{
		E:    `reverse(abc)`,
		Type: descriptor.STRING,
		IL: `
fn eval() string
  resolve_s "abc"
  call reverse
  ret
end
`,
		I: map[string]interface{}{
			"abc": "str1",
		},
		R: "1rts",
		Fns: []ast.FunctionMetadata{
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
		E:    `1.0 < 2.0`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `fn eval() bool
  apush_d 1.000000
  alt_d 2.000000
  ret
end`,
	},
	{
		E:          `1 < "a"`,
		Type:       descriptor.BOOL,
		CompileErr: `LT(1, "a") arg 2 ("a") typeError got STRING, expected INT64`,
	},
	{
		E:          `"a" < 1`,
		Type:       descriptor.BOOL,
		CompileErr: `LT("a", 1) arg 2 (1) typeError got INT64, expected STRING`,
	},
	{
		E:    `1.0 < 1.0`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `fn eval() bool
  apush_d 1.000000
  alt_d 1.000000
  ret
end`,
	},
	{
		E:    `2.0 < 1.0`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `fn eval() bool
  apush_d 2.000000
  alt_d 1.000000
  ret
end`,
	},

	{
		E:    `1.0 <= 2.0`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `fn eval() bool
  apush_d 1.000000
  ale_d 2.000000
  ret
end`,
	},
	{
		E:    `1.0 <= 1.0`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `fn eval() bool
  apush_d 1.000000
  ale_d 1.000000
  ret
end`,
	},
	{
		E:    `2.0 <= 1.0`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `fn eval() bool
  apush_d 2.000000
  ale_d 1.000000
  ret
end`,
	},

	{
		E:    `1.0 > 2.0`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `fn eval() bool
  apush_d 1.000000
  agt_d 2.000000
  ret
end`,
	},
	{
		E:    `1.0 > 1.0`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `fn eval() bool
  apush_d 1.000000
  agt_d 1.000000
  ret
end`,
	},
	{
		E:    `2.0 > 1.0`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `fn eval() bool
  apush_d 2.000000
  agt_d 1.000000
  ret
end`,
	},

	{
		E:    `1.0 >= 2.0`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `fn eval() bool
  apush_d 1.000000
  age_d 2.000000
  ret
end`,
	},
	{
		E:    `1.0 >= 1.0`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `fn eval() bool
  apush_d 1.000000
  age_d 1.000000
  ret
end`,
	},
	{
		E:    `2.0 >= 1.0`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `fn eval() bool
  apush_d 2.000000
  age_d 1.000000
  ret
end`,
	},

	{
		E:    `ad < bd`,
		Type: descriptor.BOOL,
		R:    true,
		I: map[string]interface{}{
			"ad": float64(1.0),
			"bd": float64(2.0),
		},
		IL: `fn eval() bool
  resolve_d "ad"
  resolve_d "bd"
  lt_d
  ret
end`,
	},
	{
		E:    `bd < ad`,
		Type: descriptor.BOOL,
		R:    false,
		I: map[string]interface{}{
			"ad": float64(1.0),
			"bd": float64(2.0),
		},
		IL: `fn eval() bool
  resolve_d "bd"
  resolve_d "ad"
  lt_d
  ret
end`,
	},
	{
		E:    `ad < ad`,
		Type: descriptor.BOOL,
		R:    false,
		I: map[string]interface{}{
			"ad": float64(1.0),
		},
		IL: `fn eval() bool
  resolve_d "ad"
  resolve_d "ad"
  lt_d
  ret
end`,
	},

	{
		E:    `ad <= bd`,
		Type: descriptor.BOOL,
		R:    true,
		I: map[string]interface{}{
			"ad": float64(1.0),
			"bd": float64(2.0),
		},
		IL: `fn eval() bool
  resolve_d "ad"
  resolve_d "bd"
  le_d
  ret
end`,
	},
	{
		E:    `bd <= ad`,
		Type: descriptor.BOOL,
		R:    false,
		I: map[string]interface{}{
			"ad": float64(1.0),
			"bd": float64(2.0),
		},
		IL: `fn eval() bool
  resolve_d "bd"
  resolve_d "ad"
  le_d
  ret
end`,
	},
	{
		E:    `ad <= ad`,
		Type: descriptor.BOOL,
		R:    true,
		I: map[string]interface{}{
			"ad": float64(1.0),
		},
		IL: `fn eval() bool
  resolve_d "ad"
  resolve_d "ad"
  le_d
  ret
end`,
	},

	{
		E:    `ad > bd`,
		Type: descriptor.BOOL,
		R:    false,
		I: map[string]interface{}{
			"ad": float64(1.0),
			"bd": float64(2.0),
		},
		IL: `fn eval() bool
  resolve_d "ad"
  resolve_d "bd"
  gt_d
  ret
end`,
	},
	{
		E:    `bd > ad`,
		Type: descriptor.BOOL,
		R:    true,
		I: map[string]interface{}{
			"ad": float64(1.0),
			"bd": float64(2.0),
		},
		IL: `fn eval() bool
  resolve_d "bd"
  resolve_d "ad"
  gt_d
  ret
end`,
	},
	{
		E:    `ad > ad`,
		Type: descriptor.BOOL,
		R:    false,
		I: map[string]interface{}{
			"ad": float64(1.0),
		},
		IL: `fn eval() bool
  resolve_d "ad"
  resolve_d "ad"
  gt_d
  ret
end`,
	},

	{
		E:    `ad >= bd`,
		Type: descriptor.BOOL,
		R:    false,
		I: map[string]interface{}{
			"ad": float64(1.0),
			"bd": float64(2.0),
		},
		IL: `fn eval() bool
  resolve_d "ad"
  resolve_d "bd"
  ge_d
  ret
end`,
	},
	{
		E:    `bd >= ad`,
		Type: descriptor.BOOL,
		R:    true,
		I: map[string]interface{}{
			"ad": float64(1.0),
			"bd": float64(2.0),
		},
		IL: `fn eval() bool
  resolve_d "bd"
  resolve_d "ad"
  ge_d
  ret
end`,
	},
	{
		E:    `ad >= ad`,
		Type: descriptor.BOOL,
		R:    true,
		I: map[string]interface{}{
			"ad": float64(1.0),
		},
		IL: `fn eval() bool
  resolve_d "ad"
  resolve_d "ad"
  ge_d
  ret
end`,
	},

	{
		E:    `1 > 2`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `fn eval() bool
  apush_i 1
  agt_i 2
  ret
end`,
	},

	{
		E:    `2 > 1`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `fn eval() bool
  apush_i 2
  agt_i 1
  ret
end`,
	},

	{
		E:    `1 > 1`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `fn eval() bool
  apush_i 1
  agt_i 1
  ret
end`,
	},

	{
		E:    `1 >= 2`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `fn eval() bool
  apush_i 1
  age_i 2
  ret
end`,
	},

	{
		E:    `2 >= 1`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `fn eval() bool
  apush_i 2
  age_i 1
  ret
end`,
	},

	{
		E:    `1 >= 1`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `fn eval() bool
  apush_i 1
  age_i 1
  ret
end`,
	},

	{
		E:    `1 < 2`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `fn eval() bool
  apush_i 1
  alt_i 2
  ret
end`,
	},
	{
		E:    `1 < 1`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `fn eval() bool
  apush_i 1
  alt_i 1
  ret
end`,
	},
	{
		E:    `2 < 1`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `fn eval() bool
  apush_i 2
  alt_i 1
  ret
end`,
	},
	{
		E:    `1 <= 2`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `fn eval() bool
  apush_i 1
  ale_i 2
  ret
end`,
	},
	{
		E:    `1 <= 1`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `fn eval() bool
  apush_i 1
  ale_i 1
  ret
end`,
	},
	{
		E:    `2 <= 1`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `fn eval() bool
  apush_i 2
  ale_i 1
  ret
end`,
	},
	{
		E:    `ai < bi`,
		Type: descriptor.BOOL,
		R:    true,
		I: map[string]interface{}{
			"ai": int64(1),
			"bi": int64(2),
		},
		IL: `fn eval() bool
  resolve_i "ai"
  resolve_i "bi"
  lt_i
  ret
end`,
	},
	{
		E:    `bi < ai`,
		Type: descriptor.BOOL,
		R:    false,
		I: map[string]interface{}{
			"ai": int64(1),
			"bi": int64(2),
		},
		IL: `fn eval() bool
  resolve_i "bi"
  resolve_i "ai"
  lt_i
  ret
end`,
	},
	{
		E:    `ai < ai`,
		Type: descriptor.BOOL,
		R:    false,
		I: map[string]interface{}{
			"ai": int64(1),
		},
		IL: `fn eval() bool
  resolve_i "ai"
  resolve_i "ai"
  lt_i
  ret
end`,
	},

	{
		E:    `ai > bi`,
		Type: descriptor.BOOL,
		R:    false,
		I: map[string]interface{}{
			"ai": int64(1),
			"bi": int64(2),
		},
		IL: `fn eval() bool
  resolve_i "ai"
  resolve_i "bi"
  gt_i
  ret
end`,
	},
	{
		E:    `bi > ai`,
		Type: descriptor.BOOL,
		R:    true,
		I: map[string]interface{}{
			"ai": int64(1),
			"bi": int64(2),
		},
		IL: `fn eval() bool
  resolve_i "bi"
  resolve_i "ai"
  gt_i
  ret
end`,
	},
	{
		E:    `ai > ai`,
		Type: descriptor.BOOL,
		R:    false,
		I: map[string]interface{}{
			"ai": int64(1),
		},
		IL: `fn eval() bool
  resolve_i "ai"
  resolve_i "ai"
  gt_i
  ret
end`,
	},

	{
		E:    `ai >= bi`,
		Type: descriptor.BOOL,
		R:    false,
		I: map[string]interface{}{
			"ai": int64(1),
			"bi": int64(2),
		},
		IL: `fn eval() bool
  resolve_i "ai"
  resolve_i "bi"
  ge_i
  ret
end`,
	},
	{
		E:    `bi >= ai`,
		Type: descriptor.BOOL,
		R:    true,
		I: map[string]interface{}{
			"ai": int64(1),
			"bi": int64(2),
		},
		IL: `fn eval() bool
  resolve_i "bi"
  resolve_i "ai"
  ge_i
  ret
end`,
	},
	{
		E:    `ai >= ai`,
		Type: descriptor.BOOL,
		R:    true,
		I: map[string]interface{}{
			"ai": int64(1),
		},
		IL: `fn eval() bool
  resolve_i "ai"
  resolve_i "ai"
  ge_i
  ret
end`,
	},

	{
		E:    `ai <= bi`,
		Type: descriptor.BOOL,
		R:    true,
		I: map[string]interface{}{
			"ai": int64(1),
			"bi": int64(2),
		},
		IL: `fn eval() bool
  resolve_i "ai"
  resolve_i "bi"
  le_i
  ret
end`,
	},
	{
		E:    `bi <= ai`,
		Type: descriptor.BOOL,
		R:    false,
		I: map[string]interface{}{
			"ai": int64(1),
			"bi": int64(2),
		},
		IL: `fn eval() bool
  resolve_i "bi"
  resolve_i "ai"
  le_i
  ret
end`,
	},
	{
		E:    `ai <= ai`,
		Type: descriptor.BOOL,
		R:    true,
		I: map[string]interface{}{
			"ai": int64(1),
		},
		IL: `fn eval() bool
  resolve_i "ai"
  resolve_i "ai"
  le_i
  ret
end`,
	},

	{
		E:    `"a" < "b"`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `fn eval() bool
  apush_s "a"
  alt_s "b"
  ret
end`,
	},
	{
		E:    `"b" < "a"`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `fn eval() bool
  apush_s "b"
  alt_s "a"
  ret
end`,
	},
	{
		E:    `"a" < "a"`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `fn eval() bool
  apush_s "a"
  alt_s "a"
  ret
end`,
	},

	{
		E:    `"a" <= "b"`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `fn eval() bool
  apush_s "a"
  ale_s "b"
  ret
end`,
	},
	{
		E:    `"b" <= "a"`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `fn eval() bool
  apush_s "b"
  ale_s "a"
  ret
end`,
	},
	{
		E:    `"a" <= "a"`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `fn eval() bool
  apush_s "a"
  ale_s "a"
  ret
end`,
	},

	{
		E:    `"a" > "b"`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `fn eval() bool
  apush_s "a"
  agt_s "b"
  ret
end`,
	},
	{
		E:    `"b" > "a"`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `fn eval() bool
  apush_s "b"
  agt_s "a"
  ret
end`,
	},
	{
		E:    `"a" > "a"`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `fn eval() bool
  apush_s "a"
  agt_s "a"
  ret
end`,
	},

	{
		E:    `"a" >= "b"`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `fn eval() bool
  apush_s "a"
  age_s "b"
  ret
end`,
	},
	{
		E:    `"b" >= "a"`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `fn eval() bool
  apush_s "b"
  age_s "a"
  ret
end`,
	},
	{
		E:    `"a" >= "a"`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `fn eval() bool
  apush_s "a"
  age_s "a"
  ret
end`,
	},

	{
		E:    `abc < "b"`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"abc": "a",
		},
		R: true,
		IL: `fn eval() bool
  resolve_s "abc"
  alt_s "b"
  ret
end`,
	},
	{
		E:    `"b" < abc`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"abc": "a",
		},
		R: false,
		IL: `fn eval() bool
  apush_s "b"
  resolve_s "abc"
  lt_s
  ret
end`,
	},
	{
		E:    `abc < bs`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"abc": "a",
			"bs":  "b",
		},
		R: true,
		IL: `fn eval() bool
  resolve_s "abc"
  resolve_s "bs"
  lt_s
  ret
end`,
	},
	{
		E:    `bs < abc`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"abc": "a",
			"bs":  "b",
		},
		R: false,
		IL: `fn eval() bool
  resolve_s "bs"
  resolve_s "abc"
  lt_s
  ret
end`,
	},
	{
		E:    `abc < abc`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"abc": "a",
		},
		R: false,
		IL: `fn eval() bool
  resolve_s "abc"
  resolve_s "abc"
  lt_s
  ret
end`,
	},
	{
		E:    `abc <= bs`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"abc": "a",
			"bs":  "b",
		},
		R: true,
		IL: `fn eval() bool
  resolve_s "abc"
  resolve_s "bs"
  le_s
  ret
end`,
	},
	{
		E:    `bs <= abc`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"abc": "a",
			"bs":  "b",
		},
		R: false,
		IL: `fn eval() bool
  resolve_s "bs"
  resolve_s "abc"
  le_s
  ret
end`,
	},
	{
		E:    `abc <= abc`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"abc": "a",
		},
		R: true,
		IL: `fn eval() bool
  resolve_s "abc"
  resolve_s "abc"
  le_s
  ret
end`,
	},
	{
		E:    `abc > bs`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"abc": "a",
			"bs":  "b",
		},
		R: false,
		IL: `fn eval() bool
  resolve_s "abc"
  resolve_s "bs"
  gt_s
  ret
end`,
	},
	{
		E:    `bs > abc`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"abc": "a",
			"bs":  "b",
		},
		R: true,
		IL: `fn eval() bool
  resolve_s "bs"
  resolve_s "abc"
  gt_s
  ret
end`,
	},
	{
		E:    `abc > abc`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"abc": "a",
		},
		R: false,
		IL: `fn eval() bool
  resolve_s "abc"
  resolve_s "abc"
  gt_s
  ret
end`,
	},
	{
		E:    `abc >= bs`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"abc": "a",
			"bs":  "b",
		},
		R: false,
		IL: `fn eval() bool
  resolve_s "abc"
  resolve_s "bs"
  ge_s
  ret
end`,
	},
	{
		E:    `bs >= abc`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"abc": "a",
			"bs":  "b",
		},
		R: true,
		IL: `fn eval() bool
  resolve_s "bs"
  resolve_s "abc"
  ge_s
  ret
end`,
	},
	{
		E:    `abc >= abc`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"abc": "a",
		},
		R: true,
		IL: `fn eval() bool
  resolve_s "abc"
  resolve_s "abc"
  ge_s
  ret
end`,
	},
	{
		E:    `"a" <= "b"`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `fn eval() bool
  apush_s "a"
  ale_s "b"
  ret
end`,
	},
	{
		E:    `"b" <= "a"`,
		Type: descriptor.BOOL,
		R:    false,
		IL: `fn eval() bool
  apush_s "b"
  ale_s "a"
  ret
end`,
	},
	{
		E:    `"a" <= "a"`,
		Type: descriptor.BOOL,
		R:    true,
		IL: `fn eval() bool
  apush_s "a"
  ale_s "a"
  ret
end`,
	},
	{
		E:    `abc <= abc`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"abc": "a",
		},
		R: true,
		IL: `fn eval() bool
  resolve_s "abc"
  resolve_s "abc"
  le_s
  ret
end`,
	},
	{
		E:    `abc <= bs`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"abc": "a",
			"bs":  "b",
		},
		R: true,
		IL: `fn eval() bool
  resolve_s "abc"
  resolve_s "bs"
  le_s
  ret
end`,
	},
	{
		E:    `bs <= abc`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"abc": "a",
			"bs":  "b",
		},
		R: false,
		IL: `fn eval() bool
  resolve_s "bs"
  resolve_s "abc"
  le_s
  ret
end`,
	},

	{
		E:    `abc.reverse()`,
		Type: descriptor.STRING,
		IL: `
fn eval() string
  resolve_s "abc"
  call reverse
  ret
end
`,
		I: map[string]interface{}{
			"abc": "str1",
		},
		R: "1rts",
		Fns: []ast.FunctionMetadata{
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
		// CEL reverse the arguments of matches overload
		CEL: false,
	},
	{
		E:    `"ab.*d".matches("abc")`,
		Type: descriptor.BOOL,
		R:    false,
		// CEL reverse the arguments of matches overload
		CEL: false,
	},
	{
		E:    `abc.matches(bs)`,
		Type: descriptor.BOOL,
		IL: `
fn eval() bool
  resolve_s "abc"
  resolve_s "bs"
  call matches
  ret
end`,
		I: map[string]interface{}{
			"abc": "st.*",
			"bs":  "str1",
		},
		R: true,
		// CEL reverse the arguments of matches overload
		CEL: false,
	},
	{
		E:    `abc.matches(bs)`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"abc": "st.*",
			"bs":  "sqr1",
		},
		R: false,
		// CEL reverse the arguments of matches overload
		CEL: false,
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
		E:    `abc.startsWith(bs)`,
		Type: descriptor.BOOL,
		IL: `
fn eval() bool
  resolve_s "abc"
  resolve_s "bs"
  call startsWith
  ret
end`,
		I: map[string]interface{}{
			"abc": "abcd",
			"bs":  "ab",
		},
		R: true,
	},
	{
		E:    `abc.startsWith(bs)`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"abc": "bcda",
			"bs":  "abc",
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
		E:    `abc.endsWith(bs)`,
		Type: descriptor.BOOL,
		IL: `
fn eval() bool
  resolve_s "abc"
  resolve_s "bs"
  call endsWith
  ret
end`,
		I: map[string]interface{}{
			"abc": "abcd",
			"bs":  "cd",
		},
		R: true,
	},
	{
		E:    `abc.endsWith(bs)`,
		Type: descriptor.BOOL,
		I: map[string]interface{}{
			"abc": "bcda",
			"bs":  "abc",
		},
		R: false,
	},
	{
		E:    `conditional(true, "aa", "bb")`,
		Type: descriptor.STRING,
		R:    "aa",
		IL: `
fn eval() string
  apush_b true
  jz L0
  apush_s "aa"
  jmp L1
L0:
  apush_s "bb"
L1:
  ret
end`,
	},
	{
		E:    `conditional(false, 43, 24)`,
		Type: descriptor.INT64,
		R:    int64(24),
		IL: `
fn eval() integer
  apush_b false
  jz L0
  apush_i 43
  jmp L1
L0:
  apush_i 24
L1:
  ret
end`,
	},
	{
		E:    `conditional(ab, 43.25, 25.43)`,
		Type: descriptor.DOUBLE,
		R:    float64(25.43),
		I: map[string]interface{}{
			"ab": false,
		},
		IL: `
fn eval() double
  resolve_b "ab"
  jz L0
  apush_d 43.250000
  jmp L1
L0:
  apush_d 25.430000
L1:
  ret
end`,
	},
	{
		E:    `conditional(ab, ab, bb)`,
		Type: descriptor.BOOL,
		R:    true,
		I: map[string]interface{}{
			"ab": false,
			"bb": true,
		},
		IL: `
fn eval() bool
  resolve_b "ab"
  jz L0
  resolve_b "ab"
  jmp L1
L0:
  resolve_b "bb"
L1:
  ret
end`,
	},
	{
		E:    `conditional(ab, auri, buri)`,
		Type: descriptor.URI,
		R:    "http://foo",
		I: map[string]interface{}{
			"ab":   true,
			"auri": "http://foo",
			"bt":   "http://bar",
		},
		IL: `
fn eval() string
  resolve_b "ab"
  jz L0
  resolve_s "auri"
  jmp L1
L0:
  resolve_s "buri"
L1:
  ret
end`,
	},
	{
		E:    `conditional(ab, at, bt)`,
		Type: descriptor.TIMESTAMP,
		R:    time1977,
		I: map[string]interface{}{
			"ab": false,
			"at": time1999,
			"bt": time1977,
		},
		IL: `
fn eval() interface
  resolve_b "ab"
  jz L0
  resolve_f "at"
  jmp L1
L0:
  resolve_f "bt"
L1:
  ret
end`,
	},
	{
		E:    `conditional(ab, aip, bip)`,
		Type: descriptor.IP_ADDRESS,
		R:    []byte{0x1, 0x2, 0x3, 0x4},
		I: map[string]interface{}{
			"ab":  true,
			"aip": []byte{0x1, 0x2, 0x3, 0x4},
			"bip": []byte{0x5, 0x6, 0x7, 0x8},
		},
		IL: `
fn eval() interface
  resolve_b "ab"
  jz L0
  resolve_f "aip"
  jmp L1
L0:
  resolve_f "bip"
L1:
  ret
end
`,
	},
	{
		E:    `abc | conditional(ab, "foo", "bar") | bs`,
		Type: descriptor.STRING,
		R:    "bar",
		// top-level idents do not support presence
		CEL: "",
		I: map[string]interface{}{
			"ab": false,
		},
		IL: `
fn eval() string
  tresolve_s "abc"
  jnz L0
  resolve_b "ab"
  jz L1
  apush_s "foo"
  jmp L2
L1:
  apush_s "bar"
L2:
  jmp L0
  resolve_s "bs"
L0:
  ret
end
`,
	},
	{
		E:    `abc | conditional(ab, "foo", "bar") | bs`,
		Type: descriptor.STRING,
		R:    "foo",
		// top-level idents do not support presence
		CEL: "",
		I: map[string]interface{}{
			"ab": true,
		},
	},
	{
		E:    `abc | bs | conditional(ab, "foo", "bar")`,
		Type: descriptor.STRING,
		R:    "bar",
		// top-level idents do not support presence
		CEL: "",
		I: map[string]interface{}{
			"ab": false,
		},
		IL: `
fn eval() string
  tresolve_s "abc"
  jnz L0
  tresolve_s "bs"
  jnz L0
  resolve_b "ab"
  jz L1
  apush_s "foo"
  jmp L0
L1:
  apush_s "bar"
L0:
  ret
end
`,
	},
	{
		E:    `abc | bs | conditional(ab, "foo", "bar")`,
		Type: descriptor.STRING,
		R:    "foo",
		// top-level idents do not support presence
		CEL: "",
		I: map[string]interface{}{
			"ab": true,
		},
	},
	{
		E:    `abc | bs | conditional(ab, "foo", "bar")`,
		Type: descriptor.STRING,
		R:    "boo",
		I: map[string]interface{}{
			"abc": "boo",
		},
	},
	{
		E:    `abc | bs | conditional(ab, "foo", "bar")`,
		Type: descriptor.STRING,
		R:    "zoo",
		// top-level idents do not support presence
		CEL: "",
		I: map[string]interface{}{
			"bs": "zoo",
		},
	},
	{
		E:    `conditional(ab, "foo", "bar") | abc | bs`,
		Type: descriptor.STRING,
		R:    "bar",
		I: map[string]interface{}{
			"ab": false,
		},
		IL: `
fn eval() string
  resolve_b "ab"
  jz L0
  apush_s "foo"
  jmp L1
L0:
  apush_s "bar"
L1:
  jmp L2
  tresolve_s "abc"
  jnz L2
  resolve_s "bs"
L2:
  ret
end
`,
	},
	{
		E:    `conditional(ab, "foo", "bar") | abc | bs`,
		Type: descriptor.STRING,
		R:    "foo",
		I: map[string]interface{}{
			"ab": true,
		},
	},
	{
		E:    `conditional(ab, "foo", "bar") | abc | bs`,
		Type: descriptor.STRING,
		R:    "foo",
		I: map[string]interface{}{
			"abc": "boo",
			"ab":  true,
		},
	},
	{
		E:    `conditional(ab, "foo", "bar") | abc | bs`,
		Type: descriptor.STRING,
		R:    "foo",
		I: map[string]interface{}{
			"bs": "boo",
			"ab": true,
		},
	},
	{
		E:    `conditional(ab, "foo", "bar") | abc | bs`,
		Type: descriptor.STRING,
		R:    "foo",
		I: map[string]interface{}{
			"abc": "boo",
			"bs":  "zoo",
			"ab":  true,
		},
	},
	{
		E:    `conditional(ab, "foo", "bar") | abc | bs`,
		Type: descriptor.STRING,
		R:    "bar",
		I: map[string]interface{}{
			"ab": false,
		},
	},
	{
		E:    `conditional(ab, "foo", "bar") | abc | bs`,
		Type: descriptor.STRING,
		R:    "bar",
		I: map[string]interface{}{
			"abc": "boo",
			"ab":  false,
		},
	},
	{
		E:    `conditional(ab, "foo", "bar") | abc | bs`,
		Type: descriptor.STRING,
		R:    "bar",
		I: map[string]interface{}{
			"bs": "boo",
			"ab": false,
		},
	},
	{
		E:    `conditional(ab, "foo", "bar") | abc | bs`,
		Type: descriptor.STRING,
		R:    "bar",
		I: map[string]interface{}{
			"abc": "boo",
			"bs":  "zoo",
			"ab":  false,
		},
	},
	{
		E:    `abc + bs`,
		Type: descriptor.STRING,
		R:    "ab",
		I: map[string]interface{}{
			"abc": "a",
			"bs":  "b",
		},
		IL: `
fn eval() string
  resolve_s "abc"
  resolve_s "bs"
  add_s
  ret
end
`,
	},
	{
		E:    `abc + "b"`,
		Type: descriptor.STRING,
		R:    "ab",
		I: map[string]interface{}{
			"abc": "a",
		},
		IL: `
fn eval() string
  resolve_s "abc"
  apush_s "b"
  add_s
  ret
end
`,
	},
	{
		E:    `"b" + abc`,
		Type: descriptor.STRING,
		R:    "ba",
		I: map[string]interface{}{
			"abc": "a",
		},
		IL: `
fn eval() string
  apush_s "b"
  resolve_s "abc"
  add_s
  ret
end
`,
	},
	{
		E:    `"a" + "b"`,
		Type: descriptor.STRING,
		R:    "ab",
		IL: `
fn eval() string
  apush_s "a"
  apush_s "b"
  add_s
  ret
end
`,
	},
	{
		E:    `abc | "unknown" + "b"`,
		Type: descriptor.STRING,
		R:    "ab",
		I: map[string]interface{}{
			"abc": "a",
		},
		IL: `
fn eval() string
  tresolve_s "abc"
  jnz L0
  apush_s "unknown"
L0:
  apush_s "b"
  add_s
  ret
end
`,
	},
	{
		E:    `abc | "a" + "b"`,
		Type: descriptor.STRING,
		R:    "ab",
		// top-level idents do not support presence
		CEL: "b",
	},
	{
		E:    `abc | "a" + bs + "c"`,
		Type: descriptor.STRING,
		R:    "abc",
		// top-level idents do not support presence
		CEL: "bc",
		I: map[string]interface{}{
			"bs": "b",
		},
		IL: `
fn eval() string
  tresolve_s "abc"
  jnz L0
  apush_s "a"
L0:
  resolve_s "bs"
  add_s
  apush_s "c"
  add_s
  ret
end
`,
	},
	{
		E:    `abc + (bs | "b") + "c"`,
		Type: descriptor.STRING,
		R:    "abc",
		// top-level idents do not support presence
		CEL: "ac",
		I: map[string]interface{}{
			"abc": "a",
		},
		IL: `
fn eval() string
  resolve_s "abc"
  tresolve_s "bs"
  jnz L0
  apush_s "b"
L0:
  add_s
  apush_s "c"
  add_s
  ret
end
`,
	},
	{
		E:    `ai + bi`,
		Type: descriptor.INT64,
		R:    int64(3),
		I: map[string]interface{}{
			"ai": int64(1),
			"bi": int64(2),
		},
		IL: `
fn eval() integer
  resolve_i "ai"
  resolve_i "bi"
  add_i
  ret
end
`,
	},
	{
		E:    `ai + 2`,
		Type: descriptor.INT64,
		R:    int64(3),
		I: map[string]interface{}{
			"ai": int64(1),
		},
		IL: `
fn eval() integer
  resolve_i "ai"
  apush_i 2
  add_i
  ret
end
`,
	},
	{
		E:    `ad + bd`,
		Type: descriptor.DOUBLE,
		R:    float64(3.0),
		I: map[string]interface{}{
			"ad": float64(1),
			"bd": float64(2),
		},
		IL: `
fn eval() double
  resolve_d "ad"
  resolve_d "bd"
  add_d
  ret
end
`,
	},
	{
		E:    `ad + 2.0`,
		Type: descriptor.DOUBLE,
		R:    float64(3.0),
		I: map[string]interface{}{
			"ad": float64(1),
		},
		IL: `
fn eval() double
  resolve_d "ad"
  apush_d 2.000000
  add_d
  ret
end
`,
	},
	{
		E:          `1 + "b"`,
		CompileErr: `ADD(1, "b") arg 2 ("b") typeError got STRING, expected INT64`,
	},
	{
		E: `1 + abc`,
		I: map[string]interface{}{
			"abc": "a",
		},
		CompileErr: `ADD(1, $abc) arg 2 ($abc) typeError got STRING, expected INT64`,
	},
	{
		E: `abc + 1.0`,
		I: map[string]interface{}{
			"abc": "a",
		},
		CompileErr: `ADD($abc, 1.0) arg 2 (1.0) typeError got DOUBLE, expected STRING`,
	},
	{
		E: `abc + 1.0`,
		I: map[string]interface{}{
			"abc": "a",
		},
		CompileErr: `ADD($abc, 1.0) arg 2 (1.0) typeError got DOUBLE, expected STRING`,
	},
	{
		E: `ab + bb`,
		I: map[string]interface{}{
			"ab": false,
			"bb": true,
		},
		CompileErr: `internal compiler error -- Add for type not yet implemented: bool`,
	},
	{
		E: `+abc`,
		I: map[string]interface{}{
			"abc": "a",
		},
		CompileErr: `ADD($abc) arity mismatch. Got 1 arg(s), expected 2 arg(s)`,
	},
	{
		E:    `size("x")`,
		Type: descriptor.INT64,
		R:    int64(1),
		IL: `
fn eval() integer
  apush_i 1
  ret
end
`,
	},
	{
		E:    `size(abc)`,
		Type: descriptor.INT64,
		I: map[string]interface{}{
			"abc": "two",
		},
		R: int64(3),
		IL: `
fn eval() integer
  resolve_s "abc"
  size_s
  ret
end
`,
	},
	{
		E:          `size(1)`,
		CompileErr: `size(1) arg 1 (1) typeError got INT64, expected STRING`,
	},
	{
		E:          `size()`,
		CompileErr: `size() arity mismatch. Got 0 arg(s), expected 1 arg(s)`,
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

	// CEL contains the expected result for CEL interpreter (if distinct)
	CEL interface{}

	// Referenced contains a list of attributes that should be referenced. If nil, attribute
	// tracking checks will be skipped.
	Referenced []string

	// ReferencedCEL overrides Referenced field for CEL-specific differences
	ReferencedCEL []string

	// Err contains the expected error message prefix of a failed evaluation.
	Err string

	// AstErr contains the expected error message of a failed evaluation, during AST evaluation.
	AstErr string

	// CompileErr contains the expected error message for a failed compilation.
	CompileErr string

	// Attribute manifest to use when compiling/evaluating the tests.
	// If nil, then default attributes are used.
	conf map[string]*descriptor.AttributeManifest_AttributeInfo

	// Fns field holds any additional function metadata that needs to be involved in the test.
	Fns []ast.FunctionMetadata

	// Externs holds any additional externs that should be used during evaluation.
	Externs map[string]interface{}

	// Type is the expected type of the expression upon successful compilation.
	Type descriptor.ValueType

	// SkipAst indicates that AST based evaluator should not be used for this test.
	Skipast bool

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
func (t *TestInfo) Conf() map[string]*descriptor.AttributeManifest_AttributeInfo {
	if t.conf != nil {
		return t.conf
	}
	return defaultAttrs
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

	if !attribute.Equal(t.R, r) {
		return fmt.Errorf("evaluation result mismatch: '%v' != '%v'", r, t.R)
	}

	return nil
}

// CheckReferenced will check to see if the set of referenced attributes is expected. If t.Referenced is nil
// then returns true.
func (t *TestInfo) CheckReferenced(bag *FakeBag) bool {
	if t.Referenced == nil {
		return true
	}

	actual := bag.ReferencedList()

	return reflect.DeepEqual(actual, t.Referenced)
}

// Attribute set from the original expression tests
var exprEvalAttrs = map[string]*descriptor.AttributeManifest_AttributeInfo{
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
	"destination.ip": {
		ValueType: descriptor.IP_ADDRESS,
	},
	"servicename": {
		ValueType: descriptor.STRING,
	},
	"source.labels": {
		ValueType: descriptor.STRING_MAP,
	},
}

// made up attribute set.
var defaultAttrs = map[string]*descriptor.AttributeManifest_AttributeInfo{
	"ai": {
		ValueType: descriptor.INT64,
	},
	"ab": {
		ValueType: descriptor.BOOL,
	},
	"abc": {
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
	"adns": {
		ValueType: descriptor.DNS_NAME,
	},
	"amail": {
		ValueType: descriptor.EMAIL_ADDRESS,
	},
	"auri": {
		ValueType: descriptor.URI,
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
	"bdns": {
		ValueType: descriptor.DNS_NAME,
	},
	"bmail": {
		ValueType: descriptor.EMAIL_ADDRESS,
	},
	"buri": {
		ValueType: descriptor.URI,
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
}

var istio06AttributeSet = map[string]*descriptor.AttributeManifest_AttributeInfo{
	"origin.ip":                       {ValueType: descriptor.IP_ADDRESS},
	"origin.uid":                      {ValueType: descriptor.STRING},
	"origin.user":                     {ValueType: descriptor.STRING},
	"request.headers":                 {ValueType: descriptor.STRING_MAP},
	"request.id":                      {ValueType: descriptor.STRING},
	"request.host":                    {ValueType: descriptor.STRING},
	"request.method":                  {ValueType: descriptor.STRING},
	"request.path":                    {ValueType: descriptor.STRING},
	"request.reason":                  {ValueType: descriptor.STRING},
	"request.referer":                 {ValueType: descriptor.STRING},
	"request.scheme":                  {ValueType: descriptor.STRING},
	"request.size":                    {ValueType: descriptor.INT64},
	"request.time":                    {ValueType: descriptor.TIMESTAMP},
	"request.useragent":               {ValueType: descriptor.STRING},
	"response.code":                   {ValueType: descriptor.INT64},
	"response.duration":               {ValueType: descriptor.DURATION},
	"response.headers":                {ValueType: descriptor.STRING_MAP},
	"response.size":                   {ValueType: descriptor.INT64},
	"response.time":                   {ValueType: descriptor.TIMESTAMP},
	"source.uid":                      {ValueType: descriptor.STRING},
	"source.user":                     {ValueType: descriptor.STRING},
	"destination.uid":                 {ValueType: descriptor.STRING},
	"connection.id":                   {ValueType: descriptor.STRING},
	"connection.received.bytes":       {ValueType: descriptor.INT64},
	"connection.received.bytes_total": {ValueType: descriptor.INT64},
	"connection.sent.bytes":           {ValueType: descriptor.INT64},
	"connection.sent.bytes_total":     {ValueType: descriptor.INT64},
	"connection.duration":             {ValueType: descriptor.DURATION},
	"connection.mtls":                 {ValueType: descriptor.BOOL},
	"context.protocol":                {ValueType: descriptor.STRING},
	"context.timestamp":               {ValueType: descriptor.TIMESTAMP},
	"context.time":                    {ValueType: descriptor.TIMESTAMP},
	"api.service":                     {ValueType: descriptor.STRING},
	"api.version":                     {ValueType: descriptor.STRING},
	"api.operation":                   {ValueType: descriptor.STRING},
	"api.protocol":                    {ValueType: descriptor.STRING},
	"request.auth.principal":          {ValueType: descriptor.STRING},
	"request.auth.audiences":          {ValueType: descriptor.STRING},
	"request.auth.presenter":          {ValueType: descriptor.STRING},
	"request.auth.claims":             {ValueType: descriptor.STRING_MAP},
	"request.api_key":                 {ValueType: descriptor.STRING},
	"source.ip":                       {ValueType: descriptor.IP_ADDRESS},
	"source.labels":                   {ValueType: descriptor.STRING_MAP},
	"source.name":                     {ValueType: descriptor.STRING},
	"source.namespace":                {ValueType: descriptor.STRING},
	"source.service":                  {ValueType: descriptor.STRING},
	"source.serviceAccount":           {ValueType: descriptor.STRING},
	"destination.ip":                  {ValueType: descriptor.IP_ADDRESS},
	"destination.labels":              {ValueType: descriptor.STRING_MAP},
	"destination.name":                {ValueType: descriptor.STRING},
	"destination.namespace":           {ValueType: descriptor.STRING},
	"destination.service":             {ValueType: descriptor.STRING},
	"destination.serviceAccount":      {ValueType: descriptor.STRING},
}
