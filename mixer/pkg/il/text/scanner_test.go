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

package text

import "testing"

type expect struct {
	t token
	v interface{}
}
type scTest struct {
	txt string
	e   []expect
}

var scannerTests = []scTest{
	{
		txt: `AAA`,
		e: []expect{
			{t: tkIdentifier, v: "AAA"},
		},
	},
	{
		txt: `AAA BBB `,
		e: []expect{
			{t: tkIdentifier, v: "AAA"},
			{t: tkIdentifier, v: "BBB"},
		},
	},
	{
		txt: `
	AAA
	BBB

	 CCC`,
		e: []expect{
			{t: tkNewLine},
			{t: tkIdentifier, v: "AAA"},
			{t: tkNewLine},
			{t: tkIdentifier, v: "BBB"},
			{t: tkNewLine},
			{t: tkNewLine},
			{t: tkIdentifier, v: "CCC"},
		},
	},
	{
		txt: `AAA
	`,
		e: []expect{
			{t: tkIdentifier, v: "AAA"},
			{t: tkNewLine},
		},
	},
	{
		txt: `AAA//Comment!`,
		e: []expect{
			{t: tkIdentifier, v: "AAA"},
		},
	},
	{
		txt: `AAA //Comment!`,
		e: []expect{
			{t: tkIdentifier, v: "AAA"},
		},
	},
	{
		txt: `AAA//Comment!
	`,
		e: []expect{
			{t: tkIdentifier, v: "AAA"},
			{t: tkNewLine},
		},
	},
	{
		txt: `"AAA"`,
		e: []expect{
			{t: tkStringLiteral, v: "AAA"},
		},
	},
	{
		txt: `"AA\"A"`,
		e: []expect{
			{t: tkStringLiteral, v: `AA"A`},
		},
	},
	{
		txt: `A ( B ) "C"`,
		e: []expect{
			{t: tkIdentifier, v: "A"},
			{t: tkOpenParen, v: "("},
			{t: tkIdentifier, v: "B"},
			{t: tkCloseParen, v: ")"},
			{t: tkStringLiteral, v: `C`},
		},
	},
	{
		txt: `ABC:`,
		e: []expect{
			{t: tkLabel, v: "ABC"},
		},
	},
	{
		txt: `0123`,
		e: []expect{
			{t: tkIntegerLiteral, v: int64(0123)},
		},
	},
	{
		txt: `123`,
		e: []expect{
			{t: tkIntegerLiteral, v: int64(123)},
		},
	},
	{
		txt: `0xFEB4`,
		e: []expect{
			{t: tkIntegerLiteral, v: int64(65204)},
		},
	},
	{
		txt: `0 `,
		e: []expect{
			{t: tkIntegerLiteral, v: int64(0)},
		},
	},
	{
		txt: `.123`,
		e: []expect{
			{t: tkFloatLiteral, v: float64(.123)},
		},
	},
	{
		txt: `456.123`,
		e: []expect{
			{t: tkFloatLiteral, v: float64(456.123)},
		},
	},
	{
		txt: `0456.123`,
		e: []expect{
			{t: tkFloatLiteral, v: float64(0456.123)},
		},
	},
	{
		txt: `@`,
		e: []expect{
			{t: tkError},
		},
	},
	{
		txt: `/ `,
		e: []expect{
			{t: tkError},
		},
	},
	{
		txt: `"AAA
			`,
		e: []expect{
			{t: tkError},
		},
	},
	{
		txt: `"AAA`,
		e: []expect{
			{t: tkError},
		},
	},
	{
		txt: `"AAA\
			`,
		e: []expect{
			{t: tkError},
		},
	},
	{
		txt: `"AAA\`,
		e: []expect{
			{t: tkError},
		},
	},
	{
		txt: `"AAA@`,
		e: []expect{
			{t: tkError},
		},
	},
	{
		txt: `AAA@`,
		e: []expect{
			{t: tkError},
		},
	},
	{
		txt: `123@`,
		e: []expect{
			{t: tkError},
		},
	},
	{
		txt: `0@`,
		e: []expect{
			{t: tkError},
		},
	},
	{
		txt: `123.4@`,
		e: []expect{
			{t: tkError},
		},
	},
	{
		txt: `0xABC@`,
		e: []expect{
			{t: tkError},
		},
	},
	{
		txt: ` `,
		e:   []expect{},
	},
	{
		txt: `fn()`,
		e: []expect{
			{t: tkIdentifier, v: "fn"},
			{t: tkOpenParen},
			{t: tkCloseParen},
		},
	},
	{
		txt: `fn(record)`,
		e: []expect{
			{t: tkIdentifier, v: "fn"},
			{t: tkOpenParen},
			{t: tkIdentifier, v: "record"},
			{t: tkCloseParen},
		},
	},
}

func TestScanner(t *testing.T) {
	for _, test := range scannerTests {
		t.Run("["+test.txt+"]", func(tt *testing.T) {
			s := newScanner(test.txt)
			tt.Logf("=== state ===\n%+v\n=============\n", *s)
			for i, e := range test.e {
				if !s.next() {
					tt.Fatalf("next failed when expecting token: '%+v'", e)
				}
				tt.Logf("=== state(%d) ===\n%+v\n=============\n", i, *s)

				if s.token != e.t {
					tt.Fatalf("unexpected token type encountered: E:'%v' != A:'%v'", e.t, s.token)
				}

				switch e.t {
				case tkIdentifier:
					id, f := s.asIdentifier()
					if !f {
						tt.Fatalf("unable to get token as identifier: '%v'", e.t)
					}
					if id != e.v.(string) {
						tt.Fatalf("identifier mismatch: E:'%v' != A:'%v'", e.v, id)
					}

				case tkLabel:
					id, f := s.asLabel()
					if !f {
						tt.Fatalf("unable to get token as label: '%v'", e.t)
					}
					if id != e.v.(string) {
						tt.Fatalf("label mismatch: E:'%v' != A:'%v'", e.v, id)
					}

				case tkStringLiteral:
					nl, f := s.asStringLiteral()
					if !f {
						tt.Fatalf("unable to get token as string literal: '%v'", e.t)
					}
					if nl != e.v.(string) {
						tt.Fatalf("string literal mismatch: E:'%v' != A:'%v'", e.v.(string), nl)
					}

				case tkIntegerLiteral:
					i, f := s.asIntegerLiteral()
					if !f {
						tt.Fatalf("unable to get token as integer literal: '%v'", e.t)
					}
					if i != e.v.(int64) {
						tt.Fatalf("integer literal mismatch: E:'%v' != A:'%v'", e.v.(int64), i)
					}

				case tkFloatLiteral:
					i, f := s.asFloatLiteral()
					if !f {
						tt.Fatalf("unable to get token as float literal: '%v'", e.t)
					}
					if i != e.v.(float64) {
						tt.Fatalf("integer literal mismatch: E:'%v' != A:'%v'", e.v.(float64), i)
					}

				case tkOpenParen, tkCloseParen, tkNewLine:
					var exp string
					switch e.t {
					case tkOpenParen:
						exp = "("
					case tkCloseParen:
						exp = ")"
					case tkNewLine:
						exp = "\n"
					}
					nl := s.rawText()
					if nl != exp {
						tt.Fatalf("token text mismatch: E:'%v' != A:'%v'", exp, nl)
					}

				case tkError:
					if s.token != tkError {
						tt.Fatalf("expected an error token, but got: '%v' instead", s.token)
					}

				case tkNone:
					if s.token != tkNone {
						tt.Fatalf("expected a none token, but got: '%v' instead", s.token)
					}
				default:
					tt.Fatalf("unhandled token type in test: %v", s.token)
				}
			}
			if s.token != tkError && s.next() {
				if s.token != tkNone {
					tt.Logf("=== state(final) ===\n%+v\n=============\n", *s)
					tt.Fatalf("unexpected token: %v", s.token)
				}
			}
		})
	}
}
