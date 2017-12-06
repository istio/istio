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

	"istio.io/istio/mixer/pkg/il/testing"
	"istio.io/istio/mixer/pkg/il/text"
)

// 12/6/2017
//BenchmarkIL/ExprBench/ok_1st-8         	10000000	       135 ns/op	       0 B/op	       0 allocs/op
//BenchmarkIL/ExprBench/ok_2nd-8         	 5000000	       238 ns/op	      16 B/op	       1 allocs/op
//BenchmarkIL/ExprBench/not_found-8      	 5000000	       239 ns/op	      16 B/op	       1 allocs/op
//PASS

// 5/17/2017
//ozben-macbookpro2:intr ozben$ go test -run=^$  -bench=.  -benchmem
//BenchmarkInterpreter/ASTBenchmark/[a=20,_host="abc]"-8         	20000000	       111 ns/op	       0 B/op	       0 allocs/op
//BenchmarkInterpreter/ASTBenchmark/[a=2,_host="abc]"-8          	10000000	       158 ns/op	       0 B/op	       0 allocs/op
//BenchmarkInterpreter/ASTBenchmark/[a=20,_host="abcd]"-8        	10000000	       165 ns/op	       0 B/op	       0 allocs/op
//PASS

func BenchmarkInterpreter(b *testing.B) {
	for _, test := range ilt.TestData {
		if !test.Bench {
			continue
		}

		p, err := text.ReadText(test.IL)
		if err != nil {
			b.Fatalf("Unable to parse program text: %v", err)
		}
		id := p.Functions.IDOf("eval")
		if id == 0 {
			b.Fatal("function not found: 'eval'")
		}

		bg := ilt.NewFakeBag(test.I)

		in := New(p, map[string]Extern{})

		b.Run(test.TestName(), func(bb *testing.B) {
			for i := 0; i < bb.N; i++ {
				_, _ = in.EvalFnID(id, bg)
			}
		})
	}
}
