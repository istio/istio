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

package interpreter

import (
	"testing"

	ilt "istio.io/istio/mixer/pkg/il/testing"
	"istio.io/istio/mixer/pkg/il/text"
)

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
