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

package interpreter

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"istio.io/mixer/pkg/il"
	"istio.io/mixer/pkg/il/testing"
	"istio.io/mixer/pkg/il/text"
)

// test is a common struct used by many tests in this context.
type test struct {
	// code is the program input to the interpreter, in assembly form.
	code string

	// externs is the extern input to the interpreter.
	externs map[string]Extern

	// input is the input parameters to the evaluation.
	input map[string]interface{}

	// expected is the expected result value upon successful evaluation completion.
	expected interface{}

	// err is the expected error value upon unsuccessful evaluation completion.
	err string
}

func TestInterpreter_EvalFnID(t *testing.T) {
	p, _ := text.ReadText(`
	fn main() bool
		apush_b false
		ret
	end
	`)

	i := New(p, map[string]Extern{})
	fnID := p.Functions.IDOf("main")
	r, e := i.EvalFnID(fnID, &ilt.FakeBag{})

	if e != nil {
		t.Fatal(e)
	}
	if r.AsInterface() != false {
		t.Fatalf("unexpected output from program: '%v'", r.AsInterface())
	}
}

func TestInterpreter_Eval_FunctionNotFound(t *testing.T) {
	p, _ := text.ReadText(`
	fn main() bool
		apush_b false
		ret
	end
	`)

	i := New(p, map[string]Extern{})
	_, e := i.Eval("foo", &ilt.FakeBag{})
	if e == nil {
		t.Fatal("expected error during Eval()")
	}

	if e.Error() != "function not found: 'foo'" {
		t.Fatalf("unexpected error: '%v'", e)
	}
}

func TestInterpreter_Eval(t *testing.T) {
	duration20ms, _ := time.ParseDuration("20ms")

	var tests = map[string]test{
		"halt": {
			code: `
		fn main () integer
			halt
			ret
		end`,
			err: "catching fire as instructed",
		},

		"nop": {
			code: `
		fn main () void
			nop
			ret
		end`,
			expected: nil,
		},

		"err": {
			code: `
		fn main () integer
			err "woah!"
			ret
		end`,
			err: "woah!",
		},

		"errz/0": {
			code: `
		fn main () integer
			apush_b false
			errz "woah!"
			ret
		end`,
			err: "woah!",
		},
		"errz/1": {
			code: `
		fn main () integer
			apush_b true
			errz "woah!"
			apush_i 10
			ret
		end`,
			expected: int64(10),
		},

		"errnz/0": {
			code: `
		fn main () integer
			apush_b false
			errnz "woah!"
			apush_i 10
			ret
		end`,
			expected: int64(10),
		},
		"errnz/1": {
			code: `
		fn main () integer
			apush_b true
			errnz "woah!"
			ret
		end`,
			err: "woah!",
		},

		"pop_s": {
			code: `
		fn main () string
			apush_s "foo"
			apush_s "bar"
			pop_s
			ret
		end`,
			expected: "foo",
		},

		"pop_b": {
			code: `
		fn main () bool
			apush_b true
			apush_b false
			pop_b
			ret
		end`,
			expected: true,
		},

		"pop_i": {
			code: `
		fn main () integer
			apush_i 49
			apush_i 52
			pop_i
			ret
		end`,
			expected: int64(49),
		},

		"pop_d": {
			code: `
		fn main () double
			apush_d 49.3
			apush_d 52.7
			pop_d
			ret
		end`,
			expected: float64(49.3),
		},

		"dup_s": {
			code: `
		fn main () bool
			apush_s "foo"
			dup_s
			eq_s
			ret
		end`,
			expected: true,
		},
		"dup_b": {
			code: `
		fn main () bool
			apush_b false
			dup_b
			eq_b
			ret
		end`,
			expected: true,
		},
		"dup_i": {
			code: `
		fn main () bool
			apush_i 42
			dup_i
			eq_i
			ret
		end`,
			expected: true,
		},
		"dup_d": {
			code: `
		fn main () bool
			apush_d 123.987
			dup_d
			eq_d
			ret
		end`,
			expected: true,
		},

		"rload_s": {
			code: `
		fn main () string
			apush_s "abc"
			rload_s r1
			apush_s "def"
			rpush_s r1
			ret
		end`,
			expected: "abc",
		},
		"rload_b": {
			code: `
		fn main () bool
			apush_b false
			rload_b r2
			apush_b true
			rpush_b r2
			ret
		end`,
			expected: false,
		},
		"rload_i": {
			code: `
		fn main () integer
			apush_i 42
			rload_i r2
			apush_i 54
			rpush_i r2
			ret
		end`,
			expected: int64(42),
		},
		"rload_d": {
			code: `
		fn main () double
			apush_d 42.4
			rload_d r2
			apush_d 54.6
			rpush_d r2
			ret
		end`,
			expected: float64(42.4),
		},

		"aload_s": {
			code: `
		fn main () string
			aload_s r1 "abc"
			apush_s "def"
			rpush_s r1
			ret
		end`,
			expected: "abc",
		},
		"aload_b": {
			code: `
		fn main () bool
			aload_b r2 false
			apush_b true
			rpush_b r2
			ret
		end`,
			expected: false,
		},
		"aload_i": {
			code: `
		fn main () integer
			aload_i r2 42
			apush_i 54
			rpush_i r2
			ret
		end`,
			expected: int64(42),
		},
		"aload_d": {
			code: `
		fn main () double
			aload_d r2 42.4
			apush_d 54.6
			rpush_d r2
			ret
		end`,
			expected: float64(42.4),
		},

		"apush_s": {
			code: `
		fn main () string
			apush_s "aaa"
			ret
		end`,
			expected: "aaa",
		},
		"apush_b/true": {
			code: `
		fn main () bool
			apush_b true
			ret
		end`,
			expected: true,
		},
		"apush_b/false": {
			code: `
		fn main () bool
			apush_b false
			ret
		end`,
			expected: false,
		},
		"apush_i": {
			code: `
		fn main () integer
			apush_i 20
			ret
		end`,
			expected: int64(20),
		},
		"apush_d": {
			code: `
			fn main () double
				apush_d 43.34
				ret
			end`,
			expected: float64(43.34),
		},

		"eq_s/false": {
			code: `
		fn main () bool
			apush_s "aaa"
			apush_s "bbb"
			eq_s
			ret
		end`,
			expected: false,
		},
		"eq_s/true": {
			code: `
		fn main () bool
			apush_s "aaa"
			apush_s "aaa"
			eq_s
			ret
		end`,
			expected: true,
		},
		"eq_b/false": {
			code: `
		fn main () bool
			apush_b false
			apush_b true
			eq_b
			ret
		end`,
			expected: false,
		},
		"eq_b/true": {
			code: `
		fn main () bool
			apush_b false
			apush_b false
			eq_b
			ret
		end`,
			expected: true,
		},
		"eq_i/false": {
			code: `
		fn main () bool
			apush_i 42
			apush_i 24
			eq_i
			ret
		end`,
			expected: false,
		},
		"eq_i/true": {
			code: `
		fn main () bool
			apush_i 23232
			apush_i 23232
			eq_i
			ret
		end`,
			expected: true,
		},
		"eq_d/false": {
			code: `
		fn main () bool
			apush_d 42.45
			apush_d 24.87
			eq_d
			ret
		end`,
			expected: false,
		},
		"eq_d/true": {
			code: `
		fn main () bool
			apush_d 23232.2323
			apush_d 23232.2323
			eq_d
			ret
		end`,
			expected: true,
		},

		"aeq_s/false": {
			code: `
		fn main () bool
			apush_s "aaa"
			aeq_s "bbb"
			ret
		end`,
			expected: false,
		},
		"aeq_s/true": {
			code: `
		fn main () bool
			apush_s "aaa"
			aeq_s "aaa"
			ret
		end`,
			expected: true,
		},
		"aeq_b/false": {
			code: `
		fn main () bool
			apush_b false
			aeq_b true
			ret
		end`,
			expected: false,
		},
		"aeq_b/true": {
			code: `
		fn main () bool
			apush_b false
			aeq_b false
			ret
		end`,
			expected: true,
		},
		"aeq_i/false": {
			code: `
		fn main () bool
			apush_i 42
			aeq_i 24
			ret
		end`,
			expected: false,
		},
		"aeq_i/true": {
			code: `
		fn main () bool
			apush_i 23232
			aeq_i 23232
			ret
		end`,
			expected: true,
		},
		"aeq_d/false": {
			code: `
		fn main () bool
			apush_d 42.45
			aeq_d 24.87
			ret
		end`,
			expected: false,
		},
		"aeq_d/true": {
			code: `
		fn main () bool
			apush_d 23232.2323
			aeq_d 23232.2323
			ret
		end`,
			expected: true,
		},

		"xor/f/f": {
			code: `
		fn main () bool
			apush_b false
			apush_b false
			xor
			ret
		end`,
			expected: false,
		},
		"xor/t/f": {
			code: `
		fn main () bool
			apush_b true
			apush_b false
			xor
			ret
		end`,
			expected: true,
		},
		"xor/f/t": {
			code: `
		fn main () bool
			apush_b false
			apush_b true
			xor
			ret
		end`,
			expected: true,
		},
		"xor/t/t": {
			code: `
		fn main () bool
			apush_b true
			apush_b true
			xor
			ret
		end`,
			expected: false,
		},

		"or/f/f": {
			code: `
		fn main () bool
			apush_b false
			apush_b false
			or
			ret
		end`,
			expected: false,
		},
		"or/f/t": {
			code: `
		fn main () bool
			apush_b false
			apush_b true
			or
			ret
		end`,
			expected: true,
		},
		"or/t/f": {
			code: `
		fn main () bool
			apush_b true
			apush_b false
			or
			ret
		end`,
			expected: true,
		},
		"or/t/t": {
			code: `
		fn main () bool
			apush_b true
			apush_b true
			or
			ret
		end`,
			expected: true,
		},

		"and/f/f": {
			code: `
		fn main () bool
			apush_b false
			apush_b false
			and
			ret
		end`,
			expected: false,
		},
		"and/t/f": {
			code: `
		fn main () bool
			apush_b true
			apush_b false
			and
			ret
		end`,
			expected: false,
		},
		"and/f/t": {
			code: `
		fn main () bool
			apush_b false
			apush_b true
			and
			ret
		end`,
			expected: false,
		},
		"and/t/t": {
			code: `
		fn main () bool
			apush_b true
			apush_b true
			and
			ret
		end`,
			expected: true,
		},

		"axor/f/f": {
			code: `
		fn main () bool
			apush_b false
			axor false
			ret
		end`,
			expected: false,
		},
		"axor/t/f": {
			code: `
		fn main () bool
			apush_b true
			axor false
			ret
		end`,
			expected: true,
		},
		"axor/f/t": {
			code: `
		fn main () bool
			apush_b false
			axor true
			ret
		end`,
			expected: true,
		},
		"axor/t/t": {
			code: `
		fn main () bool
			apush_b true
			axor true
			ret
		end`,
			expected: false,
		},

		"aor/f/f": {
			code: `
		fn main () bool
			apush_b false
			aor false
			ret
		end`,
			expected: false,
		},
		"aor/f/t": {
			code: `
		fn main () bool
			apush_b false
			aor true
			ret
		end`,
			expected: true,
		},
		"aor/t/f": {
			code: `
		fn main () bool
			apush_b true
			aor false
			ret
		end`,
			expected: true,
		},
		"aor/t/t": {
			code: `
		fn main () bool
			apush_b true
			aor true
			ret
		end`,
			expected: true,
		},

		"aand/f/f": {
			code: `
		fn main () bool
			apush_b false
			aand false
			ret
		end`,
			expected: false,
		},
		"aand/t/f": {
			code: `
		fn main () bool
			apush_b true
			aand false
			ret
		end`,
			expected: false,
		},
		"aand/f/t": {
			code: `
		fn main () bool
			apush_b false
			aand true
			ret
		end`,
			expected: false,
		},
		"aand/t/t": {
			code: `
		fn main () bool
			apush_b true
			aand true
			ret
		end`,
			expected: true,
		},

		"not/f": {
			code: `
		fn main () bool
			apush_b false
			not
			ret
		end`,
			expected: true,
		},
		"not/t": {
			code: `
		fn main () bool
			apush_b true
			not
			ret
		end`,
			expected: false,
		},

		"resolve_s/success": {
			code: `
		fn main () string
			resolve_s "a"
			ret
		end`,
			input: map[string]interface{}{
				"a": "z",
			},
			expected: "z",
		},
		"resolve_s/not found": {
			code: `
		fn main () string
			resolve_s "q"
			ret
		end`,
			err: "lookup failed: 'q'",
		},
		"resolve_s/type mismatch": {
			code: `
		fn main () string
			resolve_s "a"
			ret
		end`,
			input: map[string]interface{}{
				"a": true,
			},
			err: "error converting value to string: 'true'",
		},
		"resolve_b/true": {
			code: `
		fn main () bool
			resolve_b "a"
			ret
		end`,
			input: map[string]interface{}{
				"a": true,
			},
			expected: true,
		},
		"resolve_b/false": {
			code: `
		fn main () bool
			resolve_b "a"
			ret
		end`,
			input: map[string]interface{}{
				"a": false,
			},
			expected: false,
		},
		"resolve_b/not found": {
			code: `
		fn main () bool
			resolve_b "q"
			ret
		end`,
			err: "lookup failed: 'q'",
		},
		"resolve_b/type mismatch": {
			code: `
		fn main () bool
			resolve_b "a"
			ret
		end`,
			input: map[string]interface{}{
				"a": "A",
			},
			err: "error converting value to bool: 'A'",
		},
		"resolve_i/success": {
			code: `
		fn main () integer
			resolve_i "a"
			ret
		end`,
			input: map[string]interface{}{
				"a": int64(123456),
			},
			expected: int64(123456),
		},
		"resolve_i/duration/success": {
			code: `
		fn main () duration
			resolve_i "a"
			ret
		end`,
			input: map[string]interface{}{
				"a": duration20ms,
			},
			expected: duration20ms,
		},
		"resolve_i/not found": {
			code: `
		fn main () integer
			resolve_i "q"
			ret
		end`,
			err: "lookup failed: 'q'",
		},
		"resolve_i/type mismatch": {
			code: `
		fn main () integer
			resolve_i "a"
			ret
		end`,
			input: map[string]interface{}{
				"a": "B",
			},
			err: "error converting value to integer or duration: 'B'",
		},
		"resolve_d/success": {
			code: `
		fn main () double
			resolve_d "a"
			ret
		end`,
			input: map[string]interface{}{
				"a": float64(123.456),
			},
			expected: float64(123.456),
		},
		"resolve_d/not found": {
			code: `
		fn main () double
			resolve_d "q"
			ret
		end`,
			err: "lookup failed: 'q'",
		},
		"resolve_d/type mismatch": {
			code: `
		fn main () double
			resolve_d "a"
			ret
		end`,
			input: map[string]interface{}{
				"a": true,
			},
			err: "error converting value to double: 'true'",
		},
		"resolve_f/success": {
			code: `
		fn main () string
			resolve_f "a"
			alookup "b"
			ret
		end`,
			input: map[string]interface{}{
				"a": map[string]string{"b": "c"},
			},
			expected: "c",
		},
		"resolve_f/not found": {
			code: `
		fn main () string
			resolve_f "q"
			ret
		end`,
			err: "lookup failed: 'q'",
		},
		"tresolve_s/success": {
			code: `
		fn main () string
			tresolve_s "a"
			errz "not found!"
			ret
		end`,
			input: map[string]interface{}{
				"a": "z",
			},
			expected: "z",
		},
		"tresolve_s/not found": {
			code: `
		fn main () string
			tresolve_s "q"
			errz "not found!"
			ret
		end`,
			err: "not found!",
		},
		"tresolve_s/type mismatch": {
			code: `
		fn main () string
			tresolve_s "a"
			errz "not found!"
			ret
		end`,
			input: map[string]interface{}{
				"a": true,
			},
			err: "error converting value to string: 'true'",
		},
		"tresolve_b/true": {
			code: `
		fn main () bool
			tresolve_b "a"
			errz "not found!"
			ret
		end`,
			input: map[string]interface{}{
				"a": true,
			},
			expected: true,
		},
		"tresolve_b/false": {
			code: `
		fn main () bool
			tresolve_b "a"
			errz "not found!"
			ret
		end`,
			input: map[string]interface{}{
				"a": false,
			},
			expected: false,
		},
		"tresolve_b/not found": {
			code: `
		fn main () bool
			tresolve_b "q"
			errz "not found!"
			ret
		end`,
			err: "not found!",
		},
		"tresolve_b/type mismatch": {
			code: `
		fn main () bool
			tresolve_b "a"
			errz "not found!"
			ret
		end`,
			input: map[string]interface{}{
				"a": "A",
			},
			err: "error converting value to bool: 'A'",
		},
		"tresolve_i/success": {
			code: `
		fn main () integer
			tresolve_i "a"
			errz "not found!"
			ret
		end`,
			input: map[string]interface{}{
				"a": int64(123456),
			},
			expected: int64(123456),
		},
		"tresolve_i/duration/success": {
			code: `
		fn main () duration
			tresolve_i "a"
			errz "not found!"
			ret
		end`,
			input: map[string]interface{}{
				"a": duration20ms,
			},
			expected: duration20ms,
		},
		"tresolve_i/not found": {
			code: `
		fn main () integer
			tresolve_i "q"
			errz "not found!"
			ret
		end`,
			err: "not found!",
		},
		"tresolve_i/type mismatch": {
			code: `
		fn main () integer
			tresolve_i "a"
			errz "not found!"
			ret
		end`,
			input: map[string]interface{}{
				"a": "B",
			},
			err: "error converting value to integer or duration: 'B'",
		},
		"tresolve_d/success": {
			code: `
		fn main () double
			tresolve_d "a"
			errz "not found!"
			ret
		end`,
			input: map[string]interface{}{
				"a": float64(123.456),
			},
			expected: float64(123.456),
		},
		"tresolve_d/not found": {
			code: `
		fn main () double
			tresolve_d "q"
			errz "not found!"
			ret
		end`,
			err: "not found!",
		},
		"tresolve_d/type mismatch": {
			code: `
		fn main () integer
			tresolve_d "a"
			errz "not found!"
			ret
		end`,
			input: map[string]interface{}{
				"a": "B",
			},
			err: "error converting value to double: 'B'",
		},
		"tresolve_f/success": {
			code: `
		fn main () string
			tresolve_f "a"
			errz "not found!"
			alookup "b"
			ret
		end`,
			input: map[string]interface{}{
				"a": map[string]string{
					"b": "c",
				},
			},
			expected: "c",
		},
		"tresolve_f/not found": {
			code: `
		fn main () bool
			tresolve_f "q"
			errz "not found!"
		end`,
			err: "not found!",
		},
		"lookup/success": {
			code: `
		fn main () string
			resolve_f "a"
			apush_s "b"
			lookup
			ret
		end`,
			input: map[string]interface{}{
				"a": map[string]string{"b": "c"},
			},
			expected: "c",
		},
		"lookup/failure": {
			code: `
		fn main () string
			resolve_f "a"
			apush_s "q"
			lookup
			ret
		end`,
			input: map[string]interface{}{
				"a": map[string]string{"b": "c"},
			},
			err: "member lookup failed: 'q'",
		},
		"nlookup/success": {
			code: `
		fn main () string
			resolve_f "a"
			apush_s "b"
			nlookup
			ret
		end`,
			input: map[string]interface{}{
				"a": map[string]string{"b": "c"},
			},
			expected: "c",
		},
		"nlookup/not found": {
			code: `
		fn main () string
			resolve_f "a"
			apush_s "q"
			nlookup
			ret
		end`,
			input: map[string]interface{}{
				"a": map[string]string{"b": "c"},
			},
			expected: nil,
		},
		"alookup/success": {
			code: `
		fn main () string
			resolve_f "a"
			alookup "b"
			ret
		end`,
			input: map[string]interface{}{
				"a": map[string]string{"b": "c"},
			},
			expected: "c",
		},
		"alookup/failure": {
			code: `
		fn main () string
			resolve_f "a"
			alookup "q"
			ret
		end`,
			input: map[string]interface{}{
				"a": map[string]string{"b": "c"},
			},
			err: "member lookup failed: 'q'",
		},
		"anlookup/success": {
			code: `
		fn main () string
			resolve_f "a"
			anlookup "b"
			ret
		end`,
			input: map[string]interface{}{
				"a": map[string]string{"b": "c"},
			},
			expected: "c",
		},
		"alookup/not found": {
			code: `
		fn main () string
			resolve_f "a"
			anlookup "q"
			ret
		end`,
			input: map[string]interface{}{
				"a": map[string]string{"b": "c"},
			},
			expected: nil,
		},
		"tlookup/success": {
			code: `
		fn main () string
			resolve_f "a"
			apush_s "b"
			tlookup
			errz "not found!"
			ret
		end`,
			input: map[string]interface{}{
				"a": map[string]string{"b": "c"},
			},
			expected: "c",
		},
		"tlookup/failure": {
			code: `
		fn main () string
			resolve_f "a"
			apush_s "q"
			tlookup
			errz "not found!"
			ret
		end`,
			input: map[string]interface{}{
				"a": map[string]string{"b": "c"},
			},
			err: "not found!",
		},

		"add_i": {
			code: `
			fn main() integer
			  apush_i 123
			  apush_i 456
			  add_i
			  ret
		  	end`,
			expected: int64(579),
		},
		"add_i/neg/pos": {
			code: `
			fn main() integer
			  apush_i -123
			  apush_i 456
			  add_i
			  ret
		  	end`,
			expected: int64(333),
		},
		"sub_i": {
			code: `
			fn main() integer
			  apush_i 456
			  apush_i 123
			  sub_i
			  ret
		  	end`,
			expected: int64(333),
		},
		"sub_i/pos/neg": {
			code: `
			fn main() integer
			  apush_i 456
			  apush_i -123
			  sub_i
			  ret
		  	end`,
			expected: int64(579),
		},
		"aadd_i": {
			code: `
			fn main() integer
			  apush_i 456
			  aadd_i 123
			  ret
		  	end`,
			expected: int64(579),
		},
		"aadd_i/pos/neg": {
			code: `
			fn main() integer
			  apush_i 456
			  aadd_i -123
			  ret
		  	end`,
			expected: int64(333),
		},
		"asub_i": {
			code: `
			fn main() integer
			  apush_i 456
			  asub_i 123
			  ret
		  	end`,
			expected: int64(333),
		},
		"asub_i/pos/neg": {
			code: `
			fn main() integer
			  apush_i 456
			  asub_i -123
			  ret
		  	end`,
			expected: int64(579),
		},

		"add_d": {
			code: `
			fn main() double
			  apush_d 123.123
			  apush_d 456.456
			  add_d
			  ret
		  	end`,
			expected: float64(123.123) + float64(456.456),
		},
		"add_d/neg/pos": {
			code: `
			fn main() double
			  apush_d -123.123
			  apush_d 456.456
			  add_d
			  ret
		  	end`,
			expected: float64(456.456) + float64(-123.123),
		},
		"sub_d": {
			code: `
			fn main() double
			  apush_d 456.456
			  apush_d 123.123
			  sub_d
			  ret
		  	end`,
			expected: float64(456.456) - float64(123.123),
		},
		"sub_d/pos/neg": {
			code: `
			fn main() double
			  apush_d 456.456
			  apush_d -123.123
			  sub_d
			  ret
		  	end`,
			expected: float64(456.456) - float64(-123.123),
		},
		"aadd_d": {
			code: `
			fn main() double
			  apush_d 456.456
			  aadd_d 123.123
			  ret
		  	end`,
			expected: float64(456.456) + float64(123.123),
		},
		"aadd_d/pos/neg": {
			code: `
			fn main() double
			  apush_d 456.456
			  aadd_d -123.123
			  ret
		  	end`,
			expected: float64(456.456) + float64(-123.123),
		},
		"asub_d": {
			code: `
			fn main() double
			  apush_d 456.456
			  asub_d 123.123
			  ret
		  	end`,
			expected: float64(456.456) - float64(123.123),
		},
		"asub_d/pos/neg": {
			code: `
			fn main() double
			  apush_d 456.456
			  asub_d -123.123
			  ret
		  	end`,
			expected: float64(456.456) - float64(-123.123),
		},

		"jmp": {
			code: `
		fn main () bool
			apush_b true
			jmp L1
			err "grasshopper is dead!"
		L1:
			ret
		end`,
			expected: true,
		},

		"jz/yes": {
			code: `
		fn main () bool
			apush_b false
			jz L1
			err "grasshopper is dead!"
			ret
		L1:
			apush_b true
			ret
		end`,
			expected: true,
		},
		"jz/no": {
			code: `
		fn main () bool
			apush_b true
			jz L1
			apush_b true
			ret
		L1:
			err "jumping and skateboarding not allowed!"
			ret
		end`,
			expected: true,
		},

		"jnz/yes": {
			code: `
		fn main () bool
			apush_b true
			jnz L1
			err "grasshopper is dead!"
			ret
		L1:
			apush_b true
			ret
		end`,
			expected: true,
		},
		"jnz/no": {
			code: `
		fn main () bool
			apush_b false
			jnz L1
			apush_b true
			ret
		L1:
			err "jumping and skateboarding not allowed!"
			ret
		end`,
			expected: true,
		},

		"eval/return/void": {
			code: `
		fn main () void
			ret
		end`,
			expected: nil,
		},
		"eval/return/bool": {
			code: `
		fn main () bool
			apush_b true
			ret
		end`,
			expected: true,
		},
		"eval/return/string": {
			code: `
		fn main () string
			apush_s "abc"
			ret
		end`,
			expected: "abc",
		},
		"eval/return/integer42": {
			code: `
		fn main () integer
			apush_i 42
			ret
		end`,
			expected: int64(42),
		},
		"eval/return/integer0xF00000000": {
			code: `
		fn main () integer
			apush_i 0xF00000000
			ret
		end`,
			expected: int64(0xF00000000),
		},
		"call/return/void": {
			code: `
		fn main() integer
			apush_i 11
			call foo
			apush_i 12
			ret
		end

		fn foo() void
			apush_i 15
			apush_i 18
			ret
		end
		`,
			expected: int64(12),
		},
		"call/return/string": {
			code: `
		fn main() string
			call foo
			ret
		end

		fn foo() string
			apush_s "boo"
			ret
		end
		`,
			expected: "boo",
		},
		"call/return/integer1": {
			code: `
		fn main() integer
			call foo
			ret
		end

		fn foo() integer
			apush_i 0x101
			ret
		end
		`,
			expected: int64(257),
		},
		"call/return/stackcleanup1": {
			code: `
		fn main() string
			call foo
			ret
		end

		fn foo() string
			apush_s "boo"
			apush_s "bar"
			apush_s "baz"
			ret
		end
		`,
			expected: "baz",
		},
		"call/return/stackcleanup2": {
			code: `
		fn main() string
			apush_s "zoo"
			call foo
			pop_s
			ret
		end

		fn foo() string
			apush_s "boo"
			apush_s "bar"
			apush_s "baz"
			ret
		end
		`,
			expected: "zoo",
		},
		"extern/ret/string": {
			code: `
		fn main() string
			call ext
			ret
		end
		`,
			expected: "foo",
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func() string {
					return "foo"
				}),
			},
		},

		"extern/ret/int": {
			code: `
		fn main() integer
			call ext
			ret
		end
		`,
			expected: int64(42),
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func() int64 {
					return int64(42)
				}),
			},
		},

		"extern/ret/duration": {
			code: `
		fn main() duration
			call ext
			ret
		end
		`,
			expected: time.Hour,
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func() time.Duration {
					return time.Hour
				}),
			},
		},

		"extern/ret/bool": {
			code: `
		fn main() bool
			call ext
			ret
		end
		`,
			expected: true,
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func() bool {
					return true
				}),
			},
		},

		"extern/ret/double": {
			code: `
		fn main() double
			call ext
			ret
		end
		`,
			expected: float64(567.789),
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func() float64 {
					return float64(567.789)
				}),
			},
		},

		"extern/ret/string/instringmap": {
			code: `
		fn main() string
			call ext
			alookup "b"
			ret
		end
		`,
			expected: "c",
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func() map[string]string {
					return map[string]string{"b": "c"}
				}),
			},
		},

		"extern/ret/ipaddress": {
			code: `
		fn main() interface
			call ext
			ret
		end
		`,
			expected: []byte{0x1, 0x2, 0x4, 0x6},
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func() []byte {
					return []byte{0x1, 0x2, 0x4, 0x6}
				}),
			},
		},

		"extern/ret/time": {
			code: `
		fn main() interface
			call ext
			ret
		end
		`,
			expected: time.Unix(10, 25),
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func() time.Time {
					return time.Unix(10, 25)
				}),
			},
		},

		"extern/ret/void": {
			code: `
		fn main() integer
			apush_i 42
			call ext
			ret
		end
		`,
			expected: int64(42),
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func() {
				}),
			},
		},

		"extern/par/string": {
			code: `
		fn main() string
			apush_s "ABC"
			call ext
			ret
		end
		`,
			expected: "ABCDEF",
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func(s string) string {
					return s + "DEF"
				}),
			},
		},
		"extern/par/bool/true": {
			code: `
		fn main() bool
			apush_b true
			call ext
			ret
		end
		`,
			expected: false,
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func(b bool) bool {
					return !b
				}),
			},
		},
		"extern/par/bool/false": {
			code: `
		fn main() bool
			apush_b false
			call ext
			ret
		end
		`,
			expected: true,
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func(b bool) bool {
					return !b
				}),
			},
		},
		"extern/par/integer": {
			code: `
		fn main() integer
			apush_i 28
			call ext
			ret
		end
		`,
			expected: int64(56),
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func(i int64) int64 {
					return i * 2
				}),
			},
		},
		"extern/par/duration": {
			code: `
		fn main() duration
			apush_i 3601000000000 // 1 Hour + 1 Second
			call ext
			ret
		end
		`,
			expected: time.Hour + time.Second + time.Second,
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func(d time.Duration) time.Duration {
					return d + time.Second
				}),
			},
		},
		"extern/par/double": {
			code: `
		fn main() double
			apush_d 5.612
			call ext
			ret
		end
		`,
			expected: float64(11.224),
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func(d float64) float64 {
					return d * 2
				}),
			},
		},
		"extern/par/stringmap": {
			code: `
		fn main() string
			resolve_f "a"
			call ext
			ret
		end
		`,
			expected: "c",
			input: map[string]interface{}{
				"a": map[string]string{
					"b": "c",
				},
			},
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func(r map[string]string) string {
					return r["b"]
				}),
			},
		},
		"extern/par/ipaddress": {
			code: `
		fn main() interface
			resolve_f "a"
			call ext
			ret
		end
		`,
			expected: []byte{0x1, 0x2, 0x4, 0x6},
			input: map[string]interface{}{
				"a": []byte{0x1, 0x2, 0x4, 0x6},
			},
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func(b []byte) []byte {
					return b
				}),
			},
		},
		"extern/par/time": {
			code: `
		fn main() interface
			resolve_f "a"
			call ext
			ret
		end
		`,
			expected: time.Unix(10, 25),
			input: map[string]interface{}{
				"a": time.Unix(10, 25),
			},
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func(t time.Time) time.Time {
					return t
				}),
			},
		},
		"extern/err": {
			code: `
		fn main() string
			call ext
			apush_s "foo"
			ret
		end
		`,
			err: "extern failure",
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func() error {
					return errors.New("extern failure")
				}),
			},
		},
		"extern/string/err": {
			code: `
		fn main() string
			call ext
			ret
		end
		`,
			err: "extern failure",
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func() (string, error) {
					return "", errors.New("extern failure")
				}),
			},
		},
		"extern/string/err/success": {
			code: `
		fn main() string
			call ext
			ret
		end
		`,
			expected: "aaa",
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func() (string, error) {
					return "aaa", nil
				}),
			},
		},
		"extern/not_found": {
			code: `
		fn main() string
			call ext
			ret
		end
		`,
			err: "function not found: 'ext'",
		},
		"benchmark": {
			code: `
		fn main () bool
			resolve_i "a"
			aeq_i 20
			jz LeftFalse
			apush_b true
			ret
		LeftFalse:
			resolve_f "request.header"
			alookup "host"
			aeq_s "abc"
			ret
		end
		`,
			input: map[string]interface{}{
				"a": int64(19),
				"request.header": map[string]string{
					"host": "abcd",
				},
			},
			expected: false,
		},
		"benchmark/success_at_A": {
			input: map[string]interface{}{
				"a": int64(20),
				"request.header": map[string]string{
					"host": "abcd",
				},
			},
			expected: true,
		},
		"benchmark/success_at_request_header": {
			input: map[string]interface{}{
				"a": int64(19),
				"request.header": map[string]string{
					"host": "abcd",
				},
			},
			expected: false,
		},
		"non-zero main params": {
			code: `
fn main (bool) void
  apush_b true
  ret
end`,
			err: "init function must have 0 args",
		},

		// This test is to make sure that the overflow tests work correctly.
		"overflowtest": {
			code: `fn main() void
   apush_i 2
L0:
   dup_i
   aadd_i 2
   dup_i
   aeq_i 62
   jz L0
   apush_i 64
   // %s
   ret
end`,
			expected: nil,
		},

		"tlookup/invalid heap access": {
			code: `
fn main () void
	apush_b true // Prime the operand stack with "1"
	apush_s "foo"
	tlookup
end`,
			err: "invalid heap access",
		},
		"lookup/invalid heap access": {
			code: `
fn main () void
	apush_b true // Prime the operand stack with "1"
	apush_s "foo"
	lookup
end`,
			err: "invalid heap access",
		},
		"nlookup/invalid heap access": {
			code: `
fn main () void
	apush_b true // Prime the operand stack with "1"
	apush_s "foo"
	nlookup
end`,
			err: "invalid heap access",
		},
		"alookup/invalid heap access": {
			code: `
fn main () void
	apush_b true // Prime the operand stack with "1"
	alookup "foo"
end`,
			err: "invalid heap access",
		},
		"anlookup/invalid heap access": {
			code: `
fn main () void
	apush_b true // Prime the operand stack with "1"
	anlookup "foo"
end`,
			err: "invalid heap access",
		},
	}

	for n, test := range tests {
		code := test.code
		if len(test.code) == 0 {
			// Find the code from another test that has the same prefix.
			idx := strings.LastIndex(n, "/")
			if idx == -1 {
				t.Fatalf("unable parse the test name when looking for a parent: %s", n)
			}
			pn := n[0:idx]
			ptest, found := tests[pn]
			if !found {
				t.Fatalf("unable to find parent test: %s", pn)
			}
			code = ptest.code
		}

		test.code = code
		t.Run(n, func(tt *testing.T) {
			runTestCode(tt, test)
		})
	}
}

func TestInterpreter_Eval_StackUnderflow(t *testing.T) {
	var tests = map[string]test{
		"errz": {
			code: `errz "BBB"`,
		},
		"errnz": {
			code: `errnz "AAA"`,
		},
		"pop_s": {
			code: `pop_s`,
		},
		"pop_b": {
			code: `pop_b`,
		},
		"pop_i": {
			code: `pop_i`,
		},
		"pop_d": {
			code: `pop_d`,
		},
		"dup_s": {
			code: `dup_s`,
		},
		"dup_b": {
			code: `dup_b`,
		},
		"dup_i": {
			code: `dup_i`,
		},
		"dup_d": {
			code: `dup_d`,
		},
		"rload_s": {
			code: `rload_s r0`,
		},
		"rload_b": {
			code: `rload_b r0`,
		},
		"rload_i": {
			code: `rload_i r0`,
		},
		"rload_d": {
			code: `rload_d r0`,
		},
		"eq_s": {
			code: `eq_s`,
		},
		"eq_b": {
			code: `eq_b`,
		},
		"eq_i": {
			code: `eq_i`,
		},
		"eq_d": {
			code: `eq_d`,
		},
		"aeq_s": {
			code: `aeq_s "s"`,
		},
		"aeq_b": {
			code: `aeq_b true`,
		},
		"aeq_i": {
			code: `aeq_i 232`,
		},
		"aeq_d": {
			code: `aeq_d 1234.54`,
		},
		"xor": {
			code: `xor`,
		},
		"and": {
			code: `and`,
		},
		"or": {
			code: `or`,
		},
		"axor": {
			code: `axor true`,
		},
		"aand": {
			code: `aand true`,
		},
		"aor": {
			code: `aor true`,
		},
		"add_i": {
			code: `add_i`,
		},
		"sub_i": {
			code: `sub_i`,
		},
		"add_d": {
			code: `add_d`,
		},
		"sub_d": {
			code: `sub_d`,
		},
		"aadd_i": {
			code: `aadd_i 1`,
		},
		"asub_i": {
			code: `asub_i 1`,
		},
		"aadd_d": {
			code: `aadd_d 1`,
		},
		"asub_d": {
			code: `asub_d 1`,
		},
		"jz": {
			code: `
L0:
  jz L0
`,
		},
		"jnz": {
			code: `
L0:
  jnz L0
`,
		},
		"ret": {
			code: `ret`,
		},
		"not": {
			code: `not`,
		},
		"call_extern": {
			code: `call ext`,
			externs: map[string]Extern{
				"ext": ExternFromFn("ext", func(int64) {}),
			},
		},
		"lookup": {
			code: `lookup`,
		},
		"nlookup": {
			code: `nlookup`,
		},
		"tlookup": {
			code: `tlookup`,
		},
		"alookup": {
			code: `alookup "a"`,
		},
		"anlookup": {
			code: `anlookup "a"`,
		},
	}

	template := `
fn main() bool
	%s
end
`

	for n, test := range tests {
		test.code = fmt.Sprintf(template, test.code)
		test.err = "stack underflow"
		t.Run(n, func(tt *testing.T) {
			runTestCode(tt, test)
		})
	}
}

func TestInterpreter_Eval_StackUnderflow_Ret(t *testing.T) {
	var types = []string{
		"double",
		"string",
		"interface",
	}

	for _, ty := range types {
		var tst = test{
			err: "stack underflow",
			code: fmt.Sprintf(`
fn main() %s
	ret
end`, ty),
		}

		t.Run("StackUnderflow_Ret_"+ty, func(tt *testing.T) { runTestCode(tt, tst) })
	}
}

func TestInterpreter_Eval_StackOverflow(t *testing.T) {
	var tests = map[string]test{
		"rpush_s": {
			code: `rpush_s r0`,
		},
		"rpush_b": {
			code: `rpush_b r0`,
		},
		"rpush_i": {
			code: `rpush_i r0`,
		},
		"rpush_d": {
			code: `rpush_d r0`,
		},
		"dup_s": {
			code: `dup_s`,
		},
		"dup_b": {
			code: `dup_b`,
		},
		"dup_i": {
			code: `dup_i`,
		},
		"dup_d": {
			code: `dup_d`,
		},
		"apush_s": {
			code: `apush_s "AAA"`,
		},
		"apush_b": {
			code: `apush_b true`,
		},
		"apush_i": {
			code: `apush_i 1234`,
		},
		"apush_d": {
			code: `apush_d 1234`,
		},
		"resolve_s": {
			code: `resolve_s "a"`,
		},
		"resolve_b": {
			code: `resolve_b "a"`,
		},
		"resolve_i": {
			code: `resolve_i "a"`,
		},
		"resolve_d": {
			code: `resolve_d "a"`,
		},
		"resolve_f": {
			code: `resolve_f "a"`,
		},
		"tresolve_s": {
			code: `tresolve_s "a"`,
		},
		"tresolve_b": {
			code: `tresolve_b "a"`,
		},
		"tresolve_i": {
			code: `tresolve_i "a"`,
		},
		"tresolve_d": {
			code: `tresolve_d "a"`,
		},
		"tresolve_f": {
			code: `tresolve_f "a"`,
		},
	}

	template := `
fn main() void
	// Fill up stack
  apush_i 2
L0:
	dup_i
	aadd_i 2
	dup_i
	aeq_i 62
	jz L0
	apush_i 64
	%s
	ret
end
`
	for n, test := range tests {
		test.err = "stack overflow"
		test.code = fmt.Sprintf(template, test.code)

		t.Run(n, func(tt *testing.T) {
			runTestCode(tt, test)
		})
	}
}

func TestInterpreter_Eval_HeapOverflow(t *testing.T) {
	var tests = map[string]test{
		"resolve_f": {
			code: `resolve_f "a"`,
		},
		"tresolve_f": {
			code: `tresolve_f "a"`,
		},
	}

	template := `
fn main() void
   apush_i 0
L0:
	 resolve_f "a"
	 pop_b
   aadd_i 1
   dup_i
   aeq_i 63
   jz L0
   %s
   ret
end
`
	for n, test := range tests {
		test.err = "heap overflow"
		test.code = fmt.Sprintf(template, test.code)
		test.input = map[string]interface{}{
			"a": map[string]string{
				"b": "c",
			},
		}
		t.Run(n, func(tt *testing.T) {
			runTestCode(tt, test)
		})
	}
}

func runTestCode(t *testing.T, test test) {
	p := il.NewProgram()
	err := text.MergeText(test.code, p)
	if err != nil {
		t.Fatalf("code compilation failed: %v", err)
	}
	runTestProgram(t, p, test)
}

func runTestProgram(t *testing.T, p *il.Program, test test) {
	s := NewStepper(p, test.externs)

	bag := &ilt.FakeBag{Attrs: test.input}
	for err := s.Begin("main", bag); !s.Done(); s.Step() {
		if err != nil {
			t.Fatal(s.Error())
		}
		t.Log(s)
	}
	if s.Error() != nil {
		if len(test.err) == 0 {
			t.Fatal(s.Error())
		}

		if test.err != s.Error().Error() {
			t.Fatalf("errors do not match: A:'%+v' != E: '%+v'", s.Error(), test.err)
		}
	} else {
		if len(test.err) != 0 {
			t.Fatalf("expected error not found: '%+v'", test.err)
		}

		r := s.Result()
		actual := r.AsInterface()
		if !areEqual(actual, test.expected) {
			t.Fatalf("(stepper) result is not as expected: A:'%+v' != E:'%+v'", actual, test.expected)
		}
	}

	// Do the same thing with the interpreter directly:
	intr := New(p, test.externs)
	r, err := intr.Eval("main", bag)
	if err != nil {
		if len(test.err) == 0 {
			t.Fatalf("Unexpected error found: '%s'", err)
		}

		if test.err != err.Error() {
			t.Fatalf("errors do not match: A:'%+v' != E: '%+v'", err, test.err)
		}
	} else {
		if len(test.err) != 0 {
			t.Fatalf("expected error not found: '%+v'", test.err)
		}

		actual := r.AsInterface()
		if !areEqual(actual, test.expected) {
			t.Fatalf("(interpreter) result is not as expected: A:'%+v' != E:'%+v'", actual, test.expected)
		}
	}
}

func areEqual(a1 interface{}, a2 interface{}) bool {
	b1, b1Ok := a1.([]byte)
	b2, b2Ok := a2.([]byte)
	if b1Ok != b2Ok {
		return false
	}
	if b1Ok {
		return bytes.Equal(b1, b2)
	}
	return a1 == a2
}
