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

package compiled

import (
	"testing"

	ilt "istio.io/istio/mixer/pkg/il/testing"
	"istio.io/pkg/attribute"
)

// 12/6/2017
//BenchmarkCompiled/ExprBench/ok_1st-8          	10000000	       154 ns/op	       0 B/op	       0 allocs/op
//BenchmarkCompiled/ExprBench/ok_2nd-8          	 5000000	       259 ns/op	      16 B/op	       1 allocs/op
//BenchmarkCompiled/ExprBench/not_found-8       	 5000000	       258 ns/op	      16 B/op	       1 allocs/op

func BenchmarkCompiled(b *testing.B) {
	for _, test := range ilt.TestData {
		if !test.Bench {
			continue
		}

		finder := attribute.NewFinder(test.Conf())

		builder := NewBuilder(finder)
		expression, _, err := builder.Compile(test.E)
		if err != nil {
			b.Fatalf("compilation of benchmark expression failed: '%v'", err)
			return
		}

		bag := ilt.NewFakeBag(test.I)

		b.Run(test.TestName(), func(bb *testing.B) {
			for i := 0; i <= bb.N; i++ {
				_, _ = expression.Evaluate(bag)
			}
		})
	}
}
