// Copyright 2018 Istio Authors
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

package cel

import (
	"reflect"
	"strings"
	"testing"

	ilt "istio.io/istio/mixer/pkg/il/testing"
	"istio.io/istio/mixer/pkg/lang/ast"
)

func TestCEXLCompatibility(t *testing.T) {
	for _, test := range ilt.TestData {
		if test.E == "" {
			continue
		}

		t.Run(test.TestName(), func(t *testing.T) {
			t.Parallel()

			finder := ast.NewFinder(test.Conf())
			builder := NewBuilder(finder, LegacySyntaxCEL)
			ex, typ, err := builder.Compile(test.E)

			if err != nil {
				if test.CompileErr != "" {
					t.Logf("expected compile error %q, got %v", test.CompileErr, err)
					return
				}
				t.Fatalf("unexpected compile error %v for %s", err, ex)
			}

			// timestamp(2) is not a compile error in CEL
			// division is also supported by CEL
			if test.CompileErr != "" {
				t.Logf("expected compile error %q", test.CompileErr)
				return
			}

			if test.Type != typ {
				t.Errorf("expected type %s, got %s", test.Type, typ)
			}

			b := ilt.NewFakeBag(test.I)

			out, err := ex.Evaluate(b)
			if err != nil {
				if test.Err != "" {
					t.Logf("expected evaluation error %q, got %v", test.Err, err)
					return
				}
				if test.CEL != nil {
					if expectedErr, ok := test.CEL.(error); ok && strings.Contains(err.Error(), expectedErr.Error()) {
						t.Logf("expected evaluation error (override) %q, got %v", expectedErr, err)
						return
					}
				}
				t.Fatalf("unexpected evaluation error: %v", err)
			}

			expected := test.R

			// override expectation for semantic differences
			if test.CEL != nil {
				expected = test.CEL
			}

			if !reflect.DeepEqual(out, expected) {
				t.Fatalf("got %#v, expected %s (type %T and %T)", out, expected, out, expected)
			}

			// override referenced attributes value
			if test.ReferencedCEL != nil {
				test.Referenced = test.ReferencedCEL
			}

			if !test.CheckReferenced(b) {
				t.Errorf("check referenced attributes: got %v, expected %v", b.ReferencedList(), test.Referenced)
			}
		})
	}
}

func BenchmarkInterpreter(b *testing.B) {
	for _, test := range ilt.TestData {
		if !test.Bench {
			continue
		}

		finder := ast.NewFinder(test.Conf())
		builder := NewBuilder(finder, LegacySyntaxCEL)
		ex, _, _ := builder.Compile(test.E)
		bg := ilt.NewFakeBag(test.I)

		b.Run(test.TestName(), func(bb *testing.B) {
			for i := 0; i < bb.N; i++ {
				_, _ = ex.Evaluate(bg)
			}
		})
	}
}
