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

package evaluator

import (
	"testing"

	"istio.io/istio/mixer/pkg/expr"
	"istio.io/istio/mixer/pkg/il/testing"
)

// 12/6/2017
//pkg: istio.io/istio/mixer/pkg/il/evaluator
//BenchmarkEvaluator/0-ExprBench/ok_1st-8         	 3000000	       441 ns/op	       0 B/op	       0 allocs/op
//BenchmarkEvaluator/1-ExprBench/ok_2nd-8         	 3000000	       563 ns/op	      16 B/op	       1 allocs/op
//BenchmarkEvaluator/2-ExprBench/not_found-8      	 3000000	       570 ns/op	      16 B/op	       1 allocs/op
//PASS

func BenchmarkEvaluator(b *testing.B) {
	for _, test := range ilt.TestData {
		if !test.Bench {
			continue
		}

		finder := expr.NewFinder(test.Conf())

		evaluator, err := NewILEvaluator(DefaultCacheSize)
		if err != nil {
			b.Fatalf("compilation of benchmark expression failed: '%v'", err)
			return
		}
		evaluator.ChangeVocabulary(finder)

		bag := ilt.NewFakeBag(test.I)

		b.Run(test.TestName(), func(bb *testing.B) {
			for i := 0; i <= bb.N; i++ {
				_, _ = evaluator.Eval(test.E, bag)
			}
		})
	}
}
