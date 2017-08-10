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

package compiler

import (
	"fmt"
	"strings"
	"testing"
	"time"

	pbv "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/config/descriptor"
	pb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/il/interpreter"
	"istio.io/mixer/pkg/il/text"
)

type testInfo struct {
	expr   string
	code   string
	input  map[string]interface{}
	result interface{}
	err    string
}

var duration19MS, _ = time.ParseDuration("19ms")
var duration20MS, _ = time.ParseDuration("20ms")
var time1999 = time.Date(1999, time.December, 31, 23, 59, 0, 0, time.UTC)
var time1977 = time.Date(1977, time.February, 4, 12, 00, 0, 0, time.UTC)

var tests = []testInfo{
	{
		expr:   "true",
		result: true,
		code: `
fn eval() bool
  apush_b true
  ret
end`,
	},
	{
		expr:   "false",
		result: false,
		code: `
fn eval() bool
  apush_b false
  ret
end`,
	},
	{
		expr:   `"ABC"`,
		result: "ABC",
		code: `
fn eval() string
  apush_s "ABC"
  ret
end`,
	},
	{
		expr:   `456789`,
		result: int64(456789),
		code: `
fn eval() integer
  apush_i 456789
  ret
end`,
	},
	{
		expr:   `456.789`,
		result: float64(456.789),
		code: `
fn eval() double
  apush_d 456.789000
  ret
end`,
	},
	{
		expr:   `true || false`,
		result: true,
		code: `
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
		expr:   `false || true`,
		result: true,
		code: `
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
		expr:   `false || false`,
		result: false,
	},
	{
		expr:   `true || false`,
		result: true,
	},
	{
		expr:   `false || true`,
		result: true,
	},

	{
		expr:   `false || true || false`,
		result: true,
		code: `
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
		expr:   `false || false || false`,
		result: false,
	},
	{
		expr:   `false && true`,
		result: false,
		code: `
fn eval() bool
  apush_b false
  apush_b true
  and
  ret
end`,
	},
	{
		expr:   `true && false`,
		result: false,
		code: `
 fn eval() bool
  apush_b true
  apush_b false
  and
  ret
end`,
	},
	{
		expr:   `true && true`,
		result: true,
	},
	{
		expr:   `false && false`,
		result: false,
	},
	{
		expr: "b1 && b2",
		input: map[string]interface{}{
			"b1": false,
			"b2": true,
		},
		result: false,
	},
	{
		expr: "b1 && b2",
		input: map[string]interface{}{
			"b1": true,
			"b2": true,
		},
		result: true,
	},
	{
		expr:   "3 == 2",
		result: false,
		code: `
fn eval() bool
  apush_i 3
  aeq_i 2
  ret
end`,
	},

	{
		expr:   "true == false",
		result: false,
		code: `
fn eval() bool
  apush_b true
  aeq_b false
  ret
end`,
	},

	{
		expr:   "false == false",
		result: true,
	},
	{
		expr:   "false == true",
		result: false,
	},
	{
		expr:   "true == true",
		result: true,
	},

	{
		expr:   `"ABC" == "ABC"`,
		result: true,
		code: `
fn eval() bool
  apush_s "ABC"
  aeq_s "ABC"
  ret
end`,
	},

	{
		expr:   `"ABC" == "CBA"`,
		result: false,
		code: `
fn eval() bool
  apush_s "ABC"
  aeq_s "CBA"
  ret
end`,
	},

	{
		expr:   `23.45 == 45.23`,
		result: false,
		code: `
fn eval() bool
  apush_d 23.450000
  aeq_d 45.230000
  ret
end`,
	},

	{
		expr:   "3 != 2",
		result: true,
		code: `
fn eval() bool
  apush_i 3
  aeq_i 2
  not
  ret
end`,
	},

	{
		expr:   "true != false",
		result: true,
		code: `
fn eval() bool
  apush_b true
  aeq_b false
  not
  ret
end`,
	},

	{
		expr:   `"ABC" != "ABC"`,
		result: false,
		code: `
fn eval() bool
  apush_s "ABC"
  aeq_s "ABC"
  not
  ret
end`,
	},

	{
		expr:   `23.45 != 45.23`,
		result: true,
		code: `
fn eval() bool
  apush_d 23.450000
  aeq_d 45.230000
  not
  ret
end`,
	},
	{
		expr: "ai",
		err:  "lookup failed: 'ai'",
	},
	{
		expr: `ab`,
		input: map[string]interface{}{
			"ab": true,
		},
		result: true,
		code: `
fn eval() bool
  resolve_b "ab"
  ret
end`,
	},
	{
		expr: `as`,
		input: map[string]interface{}{
			"as": "AAA",
		},
		result: "AAA",
		code: `
fn eval() string
  resolve_s "as"
  ret
end`,
	},
	{
		expr: `ad`,
		input: map[string]interface{}{
			"ad": float64(23.46),
		},
		result: float64(23.46),
		code: `
fn eval() double
  resolve_d "ad"
  ret
end`,
	},
	{
		expr: `ai`,
		input: map[string]interface{}{
			"ai": int64(2346),
		},
		result: int64(2346),
		code: `
fn eval() integer
  resolve_i "ai"
  ret
end`,
	},
	{
		expr: `ar["b"]`,
		input: map[string]interface{}{
			"ar": map[string]string{
				"b": "c",
			},
		},
		result: "c",
		code: `
fn eval() string
  resolve_f "ar"
  alookup "b"
  ret
end
`,
	},
	{
		expr: `ai == 20 || ar["b"] == "c"`,
		input: map[string]interface{}{
			"ai": int64(19),
			"ar": map[string]string{
				"b": "c",
			},
		},
		result: true,
		code: `
fn eval() bool
  resolve_i "ai"
  aeq_i 20
  jz L0
  apush_b true
  ret
L0:
  resolve_f "ar"
  alookup "b"
  aeq_s "c"
  ret
end`,
	},
	{
		expr:   `as | "user1"`,
		result: "user1",
		code: `
fn eval() string
  tresolve_s "as"
  jnz L0
  apush_s "user1"
L0:
  ret
end`,
	},
	{
		expr:   `as | "user1"`,
		result: "a2",
		input: map[string]interface{}{
			"as": "a2",
		},
	},
	{
		expr:   `as | bs | "user1"`,
		result: "user1",
		code: `
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
		expr: `as | bs | "user1"`,
		input: map[string]interface{}{
			"as": "a2",
		},
		result: "a2",
	},
	{
		expr: `as | bs | "user1"`,
		input: map[string]interface{}{
			"bs": "b2",
		},
		result: "b2",
	},
	{
		expr: `as | bs | "user1"`,
		input: map[string]interface{}{
			"as": "a2",
			"bs": "b2",
		},
		result: "a2",
	},

	{
		expr: `ab | true`,
		input: map[string]interface{}{
			"ab": false,
		},
		result: false,
		code: `
 fn eval() bool
  tresolve_b "ab"
  jnz L0
  apush_b true
L0:
  ret
end`,
	},
	{
		expr:   `ab | true`,
		input:  map[string]interface{}{},
		result: true,
	},
	{
		expr: `ab | bb | true`,
		input: map[string]interface{}{
			"ab": false,
		},
		result: false,
		code: `
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
		expr: `ab | bb | true`,
		input: map[string]interface{}{
			"bb": false,
		},
		result: false,
	},
	{
		expr:   `ab | bb | true`,
		input:  map[string]interface{}{},
		result: true,
	},

	{
		expr: `ai | 42`,
		input: map[string]interface{}{
			"ai": int64(10),
		},
		result: int64(10),
		code: `
fn eval() integer
  tresolve_i "ai"
  jnz L0
  apush_i 42
L0:
  ret
end`,
	},
	{
		expr:   `ai | 42`,
		input:  map[string]interface{}{},
		result: int64(42),
	},
	{
		expr: `ai | bi | 42`,
		input: map[string]interface{}{
			"ai": int64(10),
		},
		result: int64(10),
		code: `
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
		expr: `ai | bi | 42`,
		input: map[string]interface{}{
			"bi": int64(20),
		},
		result: int64(20),
	},
	{
		expr:   `ai | bi | 42`,
		input:  map[string]interface{}{},
		result: int64(42),
	},

	{
		expr: `ad | 42.1`,
		input: map[string]interface{}{
			"ad": float64(10),
		},
		result: float64(10),
		code: `
fn eval() double
  tresolve_d "ad"
  jnz L0
  apush_d 42.100000
L0:
  ret
end`,
	},
	{
		expr:   `ad | 42.1`,
		input:  map[string]interface{}{},
		result: float64(42.1),
	},
	{
		expr: `ad | bd | 42.1`,
		input: map[string]interface{}{
			"ad": float64(10),
		},
		result: float64(10),
		code: `
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
		expr: `ad | bd | 42.1`,
		input: map[string]interface{}{
			"bd": float64(20),
		},
		result: float64(20),
	},
	{
		expr:   `ad | bd | 42.1`,
		input:  map[string]interface{}{},
		result: float64(42.1),
	},

	{
		expr: `(ar | br)["foo"]`,
		input: map[string]interface{}{
			"ar": map[string]string{
				"foo": "bar",
			},
			"br": map[string]string{
				"foo": "far",
			},
		},
		result: "bar",
		code: `
fn eval() string
  tresolve_f "ar"
  jnz L0
  resolve_f "br"
L0:
  alookup "foo"
  ret
end`,
	},
	{
		expr: `(ar | br)["foo"]`,
		input: map[string]interface{}{
			"br": map[string]string{
				"foo": "far",
			},
		},
		result: "far",
	},

	{
		expr: "ai == 2",
		input: map[string]interface{}{
			"ai": int64(2),
		},
		result: true,
		code: `
 fn eval() bool
  resolve_i "ai"
  aeq_i 2
  ret
end`,
	},
	{
		expr: "ai == 2",
		input: map[string]interface{}{
			"ai": int64(0x7F000000FF000000),
		},
		result: false,
	},
	{
		expr: "as == bs",
		input: map[string]interface{}{
			"as": "ABC",
			"bs": "ABC",
		},
		result: true,
		code: `
fn eval() bool
  resolve_s "as"
  resolve_s "bs"
  eq_s
  ret
end`,
	},
	{
		expr: "ab == bb",
		input: map[string]interface{}{
			"ab": true,
			"bb": true,
		},
		result: true,
		code: `
fn eval() bool
  resolve_b "ab"
  resolve_b "bb"
  eq_b
  ret
end`,
	},
	{
		expr: "ai == bi",
		input: map[string]interface{}{
			"ai": int64(0x7F000000FF000000),
			"bi": int64(0x7F000000FF000000),
		},
		result: true,
		code: `
fn eval() bool
  resolve_i "ai"
  resolve_i "bi"
  eq_i
  ret
end`,
	},
	{
		expr: "ad == bd",
		input: map[string]interface{}{
			"ad": float64(345345.45),
			"bd": float64(345345.45),
		},
		result: true,
		code: `
fn eval() bool
  resolve_d "ad"
  resolve_d "bd"
  eq_d
  ret
end`,
	},
	{
		expr: "ai != 2",
		input: map[string]interface{}{
			"ai": int64(2),
		},
		result: false,
		code: `
 fn eval() bool
  resolve_i "ai"
  aeq_i 2
  not
  ret
end`,
	},

	{
		expr: `sm["foo"]`,
		input: map[string]interface{}{
			"sm": map[string]string{"foo": "bar"},
		},
		result: "bar",
		code: `
fn eval() string
  resolve_f "sm"
  alookup "foo"
  ret
end`,
	},
	{
		expr: `sm[as]`,
		input: map[string]interface{}{
			"as": "foo",
			"sm": map[string]string{"foo": "bar"},
		},
		result: "bar",
		code: `
fn eval() string
  resolve_f "sm"
  resolve_s "as"
  lookup
  ret
end`,
	},
	{
		expr:   `ar["c"] | "foo"`,
		input:  map[string]interface{}{},
		result: "foo",
		code: `
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
		expr: `ar["c"] | "foo"`,
		input: map[string]interface{}{
			"ar": map[string]string{"c": "b"},
		},
		result: "b",
	},
	{
		expr:   `ar[as] | "foo"`,
		input:  map[string]interface{}{},
		result: "foo",
		code: `
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
		expr: `ar[as] | "foo"`,
		input: map[string]interface{}{
			"ar": map[string]string{"as": "bar"},
		},
		result: "foo",
	},
	{
		expr: `ar[as] | "foo"`,
		input: map[string]interface{}{
			"as": "bar",
		},
		result: "foo",
	},
	{
		expr: `ar[as] | "foo"`,
		input: map[string]interface{}{
			"ar": map[string]string{"as": "bar"},
			"as": "!!!!",
		},
		result: "foo",
	},
	{
		expr: `ar[as] | "foo"`,
		input: map[string]interface{}{
			"ar": map[string]string{"asval": "bar"},
			"as": "asval",
		},
		result: "bar",
	},
	{
		expr: `ar["b"] | ar["c"] | "null"`,
		input: map[string]interface{}{
			"ar": map[string]string{
				"b": "c",
				"c": "b",
			},
		},
		result: "c",
		code: `
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
		expr: `ar["b"] | ar["c"] | "null"`,
		input: map[string]interface{}{
			"ar": map[string]string{},
		},
		result: "null",
	},
	{
		expr: `ar["b"] | ar["c"] | "null"`,
		input: map[string]interface{}{
			"ar": map[string]string{
				"b": "c",
			},
		},
		result: "c",
	},
	{
		expr: `ar["b"] | ar["c"] | "null"`,
		input: map[string]interface{}{
			"ar": map[string]string{
				"c": "b",
			},
		},
		result: "b",
	},
	{
		expr: `adur`,
		input: map[string]interface{}{
			"adur": duration20MS,
		},
		result: duration20MS,
		code: `
fn eval() duration
  resolve_i "adur"
  ret
end`,
	},
	{
		expr:   `adur | "19ms"`,
		input:  map[string]interface{}{},
		result: duration19MS,
		code: `
fn eval() duration
  tresolve_i "adur"
  jnz L0
  apush_i 19000000
L0:
  ret
end`,
	},
	{
		expr: `adur | "19ms"`,
		input: map[string]interface{}{
			"adur": duration20MS,
		},
		result: duration20MS,
	},
	{
		expr: `at`,
		input: map[string]interface{}{
			"at": time1977,
		},
		result: time1977,
	},
	{
		expr: `at | bt`,
		input: map[string]interface{}{
			"at": time1999,
			"bt": time1977,
		},
		result: time1999,
	},
	{
		expr: `at | bt`,
		input: map[string]interface{}{
			"bt": time1977,
		},
		result: time1977,
	},
	{
		expr: `aip`,
		input: map[string]interface{}{
			"aip": []byte{0x1, 0x2, 0x3, 0x4},
		},
		result: []byte{0x1, 0x2, 0x3, 0x4},
	},
	{
		expr: `aip | bip`,
		input: map[string]interface{}{
			"bip": []byte{0x4, 0x5, 0x6, 0x7},
		},
		result: []byte{0x4, 0x5, 0x6, 0x7},
	},
	{
		expr: `aip | bip`,
		input: map[string]interface{}{
			"aip": []byte{0x1, 0x2, 0x3, 0x4},
			"bip": []byte{0x4, 0x5, 0x6, 0x7},
		},
		result: []byte{0x1, 0x2, 0x3, 0x4},
	},
}

var globalConfig = pb.GlobalConfig{
	Manifests: []*pb.AttributeManifest{
		{
			Attributes: map[string]*pb.AttributeManifest_AttributeInfo{
				"ai": {
					ValueType: pbv.INT64,
				},
				"ab": {
					ValueType: pbv.BOOL,
				},
				"as": {
					ValueType: pbv.STRING,
				},
				"ad": {
					ValueType: pbv.DOUBLE,
				},
				"ar": {
					ValueType: pbv.STRING_MAP,
				},
				"adur": {
					ValueType: pbv.DURATION,
				},
				"at": {
					ValueType: pbv.TIMESTAMP,
				},
				"aip": {
					ValueType: pbv.IP_ADDRESS,
				},
				"bi": {
					ValueType: pbv.INT64,
				},
				"bb": {
					ValueType: pbv.BOOL,
				},
				"bs": {
					ValueType: pbv.STRING,
				},
				"bd": {
					ValueType: pbv.DOUBLE,
				},
				"br": {
					ValueType: pbv.STRING_MAP,
				},
				"bdur": {
					ValueType: pbv.DURATION,
				},
				"bt": {
					ValueType: pbv.TIMESTAMP,
				},
				"bip": {
					ValueType: pbv.IP_ADDRESS,
				},
				"b1": {
					ValueType: pbv.BOOL,
				},
				"b2": {
					ValueType: pbv.BOOL,
				},
				"sm": {
					ValueType: pbv.STRING_MAP,
				},
			},
		},
	},
}

func TestCompile(t *testing.T) {

	finder := descriptor.NewFinder(&globalConfig)

	for i, te := range tests {
		t.Run(fmt.Sprintf("%d '%s'", i, te.expr), func(tt *testing.T) {
			result, err := Compile(te.expr, finder)
			if err != nil {
				tt.Fatalf("error received during compile: %v", err)
			}
			actual := text.WriteText(result.Program)
			if len(te.code) > 0 {
				if strings.TrimSpace(actual) != strings.TrimSpace(te.code) {
					tt.Log("===== EXPECTED ====\n")
					tt.Log(te.code)
					tt.Log("\n====== ACTUAL =====\n")
					tt.Log(actual)
					tt.Log("===================\n")
					tt.Fail()
					return
				}
			}
			b := bag{attrs: te.input}
			i := interpreter.New(result.Program, map[string]interpreter.Extern{})
			v, err := i.Eval("eval", &b)
			if err != nil {
				if len(te.err) != 0 {
					if te.err != err.Error() {
						tt.Fatalf("expected error not found: E:'%v', A:'%v'", te.err, err)
					}
				} else {
					tt.Fatal(err)
				}
				return
			}

			if len(te.err) != 0 {
				tt.Fatalf("expected error not received: '%v'", te.err)
			}

			// Byte arrays are not comparable natively
			bExp, found := te.result.([]byte)
			if found {
				bAct, found := v.Interface().([]byte)
				if !found || !bytesEqual(bExp, bAct) {
					tt.Fatalf("Result match failed: %+v == %+v", v.Interface(), te.result)
				}
			} else if v.Interface() != te.result {
				tt.Fatalf("Result match failed: %+v == %+v", v.Interface(), te.result)
			}
		})
	}
}

func bytesEqual(b1 []byte, b2 []byte) bool {
	if len(b1) != len(b2) {
		return false
	}
	for i := 0; i < len(b1); i++ {
		if b1[i] != b2[i] {
			return false
		}
	}

	return true
}

func TestCompile_ParseError(t *testing.T) {

	finder := descriptor.NewFinder(&globalConfig)
	_, err := Compile(`@23`, finder)
	if err == nil {
		t.Fatal()
	}
	if err.Error() != "unable to parse expression '@23': 1:1: illegal character U+0040 '@'" {
		t.Fatalf("error is not as expected: '%v'", err)
	}
}

func TestCompile_TypeError(t *testing.T) {

	finder := descriptor.NewFinder(&globalConfig)
	_, err := Compile(`ai == true`, finder)
	if err == nil {
		t.Fatal()
	}
	if err.Error() != "EQ($ai, true) arg 2 (true) typeError got BOOL, expected INT64" {
		t.Fatalf("error is not as expected: '%v'", err)
	}
}

// fake bag
type bag struct {
	attrs map[string]interface{}
}

func (b *bag) Get(name string) (interface{}, bool) {
	c, found := b.attrs[name]
	return c, found
}

func (b *bag) Names() []string {
	return []string{}
}

func (b *bag) Done() {
}
