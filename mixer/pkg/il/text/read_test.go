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

import (
	"strings"
	"testing"
)

type readTest struct {
	i   string
	e   string
	err string
}

var readTests = []readTest{
	{
		i: ``,
	},
	{
		i: `fn main() bool
end`,
	},
	{
		i: `
fn main(integer) string
end`,
	},
	{
		i: `
fn main ( integer )string
end`,
		e: `
fn main(integer) string
end`,
	},
	{
		i: `
fn main(interface integer) string
end`,
	},
	{
		i: `
fn foo() interface
  ret
end

fn bar() bool
  ret
end
`,
		e: `
fn bar() bool
  ret
end

fn foo() interface
  ret
end
`,
	},
	{
		i: `
		// You get a comment
		fn main () bool // You get a comment
		// You get a comment
			apush_i 0//You get a comment
			ret//You get a comment
			ret // You get a comment
		end // You get a comment
		// Everyone gets a comment
		`,
		e: `
fn main() bool
  apush_i 0
  ret
  ret
end`,
	},
	{
		i: `
fn main() bool
  err "What do you mean \"that\"?"
  ret
end
`,
	},
	{
		i: `
fn main() bool
  apush_b true
  apush_b false
  ret
end`,
	},
	{
		i: `
fn main() integer
  apush_i 42
  ret
end`,
	},
	{
		i: `
fn main() integer
  apush_i 0x42
  ret
end`,
		e: `
fn main() integer
  apush_i 66
  ret
end`,
	},
	{
		i: `
fn main() integer
  apush_i 0xFF
  ret
end`,
		e: `
fn main() integer
  apush_i 255
  ret
end`,
	},
	{
		i: `
fn main() integer
  apush_i 0xFF00000000
  ret
end`,
		e: `
fn main() integer
  apush_i 1095216660480
  ret
end`,
	},
	{
		i: `
fn main() integer
  apush_i -53
  ret
end`,
		e: `
fn main() integer
  apush_i -53
  ret
end`,
	},
	{
		i: `
fn main() bool
  call main
  ret
end`,
	},
	{
		i: `
fn main() bool
  rload_i r0
  ret
end
`,
	},
	{
		i: `
fn main() double
  apush_d 234.567
  ret
end`,
		e: `
fn main() double
  apush_d 234.567000
  ret
end
		`,
	},
	{
		i: `
fn main() double
  apush_d -234.567
  ret
end`,
		e: `
fn main() double
  apush_d -234.567000
  ret
end
		`,
	},
	{
		i: `
fn main() bool
LBL:
  rload_i r0
  jmp LBL
  ret
end
`,
		e: `
fn main() bool
L0:
  rload_i r0
  jmp L0
  ret
end
`,
	},

	{i: ` 23 fn`, err: `unexpected input: '23' @(L: 1, C: 2)`},
	{i: `fn main AAA ( AA`, err: `unexpected input: 'AAA' @(L: 1, C: 9)`},
	{i: `fn main ( 23 )`, err: `unexpected input: '23' @(L: 1, C: 11)`},
	{i: `fn main() 23 `, err: `unexpected input: '23' @(L: 1, C: 11)`},
	{i: `fn main() twentythree `, err: `Unrecognized return type: 'twentythree' @(L: 1, C: 11)`},
	{i: `fn main ( plum )`, err: `Unrecognized parameter type: 'plum' @(L: 1, C: 11)`},
	{i: ` @`, err: `Parse error. @(L: 1, C: 2)`},
	{i: `fn /`, err: `Parse error. @(L: 1, C: 5)`},
	{i: ` Creme Brulee`, err: `Expected 'fn'. @(L: 1, C: 2)`},
	{
		i: `
fn main() bool
  23
end`,
		err: `unexpected input: '23' @(L: 3, C: 3)`,
	},
	{
		i: `
fn main() bool
  err 23
end`,
		err: `unexpected input: '23' @(L: 3, C: 7)`,
	},
	{
		i: `
fn main() bool
  aload_d "AAA"
end`,
		err: `unexpected input: '"AAA"' @(L: 3, C: 11)`,
	},
	{
		i: `
fn main() bool
  apush_s "AAA" end
  `,
		err: `unexpected input: 'end' @(L: 3, C: 17)`,
	},
	{
		i: `
		fn main () bool
 L:`,
		err: `unexpected end of file. @(L: 3, C: 4)`,
	},
	{
		i: `
		fn main () bool
  err "Don't interru
  upt"
  ret`,
		err: `Parse error. @(L: 3, C: 21)`,
	},
	{
		i: `
fn main () bool
  err "I said don`,
		err: `Parse error. @(L: 3, C: 18)`,
	},
	{
		i: `
fn main () bool
  err "What part of \"Don't interrupt\`,
		err: `Parse error. @(L: 3, C: 39)`,
	},
	{
		i: `
fn main () bool
  apush_i 0a
  ret
end
	`,
		err: "Parse error. @(L: 3, C: 12)",
	},
	{
		i: `
fn main () bool
  apush_i "aaa"
  ret
end
	`,
		err: `unexpected input: '"aaa"' @(L: 3, C: 11)`,
	},
	{
		i: `
fn main () bool
  apush_b blue
  ret
end
		`,
		err: "unexpected input: 'blue' @(L: 3, C: 11)",
	},
	{
		i: `
fn main () bool
  apush_b 23
  ret
end
		`,
		err: "unexpected input: '23' @(L: 3, C: 11)",
	},
	{
		i: `
fn main () bool
  apush_d "AAA"
  ret
end
		`,
		err: `unexpected input: '"AAA"' @(L: 3, C: 11)`,
	},
	{
		i: `
fn main () bool
  glue
  ret
end
		`,
		err: "unrecognized opcode: 'glue' @(L: 3, C: 3)",
	},
	{
		i: `
fn main () bool
  jmp ABYSS
  ret
end`,
		err: "Label not found: ABYSS @(L: 3, C: 7)",
	},
	{
		i: `
fn main () bool boo
  ret
end
	`,
		err: "unexpected input: 'boo' @(L: 2, C: 17)",
	},
	{
		i: `
fn main () bool
  jmp 23
end`,
		err: "unexpected input: '23' @(L: 3, C: 7)",
	},
	{
		i: `
fn main () bool
  rload_i 23
end
		`,
		err: "unexpected input: '23' @(L: 3, C: 11)",
	},
	{
		i: `
fn main () bool
  rload_i z23
end
		`,
		err: "Invalid register name: 'z23' @(L: 3, C: 11)",
	},
	{
		i: `
fn main () bool
  rload_i r23z
end
		`,
		err: "Invalid register name: 'r23z' @(L: 3, C: 11)",
	},
	{
		i: `
fn main () bool
  rload_i z
end
		`,
		err: "Invalid register name: 'z' @(L: 3, C: 11)",
	},
	{
		i: `
fn main () bool
  jmp 23
end
		`,
		err: "unexpected input: '23' @(L: 3, C: 7)",
	},
	{
		i: `
fn main () bool
  call 23
end
		`,
		err: "unexpected input: '23' @(L: 3, C: 8)",
	},
	{
		i: `
fn main (ha) bool
  call 23
end
		`,
		err: "Unrecognized parameter type: 'ha' @(L: 2, C: 10)",
	},
	{
		i: `
fn main (string !) bool
  call 23
end
		`,
		err: "Parse error. @(L: 2, C: 17)",
	},
}

func TestRead(t *testing.T) {
	for _, test := range readTests {
		t.Run("["+test.i+"]", func(tt *testing.T) {
			if len(test.e) == 0 {
				test.e = test.i
			}
			p, err := ReadText(test.i)
			if err != nil {
				if len(test.err) == 0 {
					tt.Fatalf("unexpected error: %v", err)
				} else if err.Error() != test.err {
					tt.Fatalf("error mismatch: E:'%s', A:'%s'", test.err, err.Error())
				}
				return
			}
			if len(test.err) != 0 {
				tt.Fatalf("error was expected: %s", test.err)
			}

			a := WriteText(p)
			if strings.TrimSpace(a) != strings.TrimSpace(test.e) {
				tt.Logf("Input: \n%s\n\n", test.i)
				tt.Logf("Expected: \n%s\n\n", test.e)
				tt.Logf("Actual: \n%s\n\n", a)
				tt.Fatal()
			}
		})
	}
}
