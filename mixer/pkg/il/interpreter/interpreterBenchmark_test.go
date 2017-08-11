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
	"testing"

	pbv "istio.io/api/mixer/v1/config/descriptor"
	pb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/il/testing"
	"istio.io/mixer/pkg/il/text"
)

// 5/17/2017
//ozben-macbookpro2:intr ozben$ go test -run=^$  -bench=.  -benchmem
//BenchmarkInterpreter/ASTBenchmark/[a=20,_host="abc]"-8         	20000000	       111 ns/op	       0 B/op	       0 allocs/op
//BenchmarkInterpreter/ASTBenchmark/[a=2,_host="abc]"-8          	10000000	       158 ns/op	       0 B/op	       0 allocs/op
//BenchmarkInterpreter/ASTBenchmark/[a=20,_host="abcd]"-8        	10000000	       165 ns/op	       0 B/op	       0 allocs/op
//PASS

type benchmarkProgram struct {
	name       string
	expression string
	code       string
	config     pb.GlobalConfig
}

var programs = map[string]benchmarkProgram{

	"ASTBenchmark": {
		name:       "ASTBenchmark",
		expression: `a == 20 || request.header["host"] == "abc"`,
		config: pb.GlobalConfig{
			Manifests: []*pb.AttributeManifest{
				{
					Attributes: map[string]*pb.AttributeManifest_AttributeInfo{
						"a": {
							ValueType: pbv.INT64,
						},
						"request.header": {
							ValueType: pbv.STRING_MAP,
						},
					},
				},
			},
		},
		code: `
fn eval() bool
  resolve_i "a"
  ieq_i 20
  jz L0
  ipush_b true
  ret
L0:
  resolve_r "request.header"
  ilookup "host"
  ieq_s "abc"
  ret
end`,
	},
}

type benchmarkTest struct {
	program benchmarkProgram
	attrs   map[string]interface{}
	result  interface{}
}

var interpreterBenchmarkTests = map[string]benchmarkTest{
	`ExprBench/ok_1st`: {
		program: programs["ASTBenchmark"],
		attrs: map[string]interface{}{
			"a": int64(20),
			"request.header": map[string]string{
				"host": "abc",
			},
		},
		result: true,
	},

	`ExprBench/ok_2nd`: {
		program: programs["ASTBenchmark"],
		attrs: map[string]interface{}{
			"a": int64(2),
			"request.header": map[string]string{
				"host": "abc",
			},
		},
		result: true,
	},

	`ExprBench/not_found`: {
		program: programs["ASTBenchmark"],
		attrs: map[string]interface{}{
			"a": int64(2),
			"request.header": map[string]string{
				"host": "abcd",
			},
		},
		result: false,
	},
}

func BenchmarkIL(b *testing.B) {
	for n, bt := range interpreterBenchmarkTests {
		p, err := text.ReadText(bt.program.code)
		if err != nil {
			b.Fatalf("Unable to parse program text: %v", err)
		}
		id := p.Functions.IDOf("eval")
		if id == 0 {
			b.Fatal("function not found: 'eval'")
		}

		bg := &ilt.FakeBag{Attrs: bt.attrs}

		in := New(p, map[string]Extern{})

		r, e := in.EvalFnID(id, bg)
		if e != nil {
			b.Fatalf("evaluation failed: '%v'", e)
		}
		if r.AsInterface() != bt.result {
			b.Fatalf("expected result not found: E:'%v' != A:'%v'", bt.result, r)
		}

		b.Run(n, func(bb *testing.B) {
			for i := 0; i < bb.N; i++ {
				_, _ = in.EvalFnID(id, bg)
			}
		})
	}
}

func BenchmarkExpr(b *testing.B) {
	for n, bt := range interpreterBenchmarkTests {
		exf, _ := expr.Parse(bt.program.expression)
		fm := expr.FuncMap()

		bg := &ilt.FakeBag{Attrs: bt.attrs}

		r, e := exf.Eval(bg, fm)
		if e != nil {
			b.Fatalf("evaluation failed: '%v'", e)
		}
		if r != bt.result {
			b.Fatalf("expected result not found: E:'%v' != A:'%v'", bt.result, r)
		}
		b.Run(n, func(bb *testing.B) {
			for i := 0; i < bb.N; i++ {
				_, _ = exf.Eval(bg, fm)
			}
		})
	}
}
