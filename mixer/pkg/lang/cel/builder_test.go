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
	"testing"

	ilt "istio.io/istio/mixer/pkg/il/testing"
	"istio.io/istio/mixer/pkg/lang/ast"
)

func TestCEXLCompatibility(t *testing.T) {
	t.Skip()
	for _, test := range ilt.TestData {
		if test.E == "" {
			continue
		}

		t.Run(test.TestName(), func(t *testing.T) {
			converted, err := sourceCEXLToCEL(test.E)
			if err != nil {
				if test.AstErr != "" {
					t.Logf("expected parse error %q, got %v", test.AstErr, err)
					return
				}
				t.Fatal(err)
			}

			finder := ast.NewFinder(test.Conf())
			builder := NewBuilder(finder)
			ex, typ, err := builder.Compile(converted)

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
				t.Fatalf("unexpected evaluation error: %v", err)
			}

			if !reflect.DeepEqual(out, test.R) {
				t.Fatalf("got %s, expected %s", out, test.R)
			}
		})
	}
}
