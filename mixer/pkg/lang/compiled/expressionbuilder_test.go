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

	istio_mixer_v1_config_descriptor "istio.io/api/policy/v1beta1"
	ilt "istio.io/istio/mixer/pkg/il/testing"
	"istio.io/pkg/attribute"
)

func TestCompiledExpressions(t *testing.T) {
	for _, test := range ilt.TestData {
		if test.E == "" {
			// Skip tests that don't have expression.
			continue
		}

		if test.Fns != nil {
			// Skip tests that have extern functions defined. We cannot inject extern functions into the evaluator.
			// Compiler tests actually also do evaluation.
			continue
		}

		name := "Compiled/" + test.TestName()
		t.Run(name, func(tt *testing.T) {
			finder := attribute.NewFinder(test.Conf())

			builder := NewBuilder(finder)
			compiled, exprType, err := builder.Compile(test.E)
			if test.CompileErr != "" {
				if err == nil {
					tt.Fatalf("expected compile error not found: '%v'", test.CompileErr)
					return
				}
				if err.Error() != test.CompileErr {
					tt.Fatalf("compile error mismatch: '%v' != '%v'", err, test.CompileErr)
				}
				return
			} else if err != nil {
				tt.Fatalf("unexpected compile error: '%v'", err)
				return
			}

			if exprType != test.Type {
				tt.Fatalf("expression type mismatch: '%v' != '%v'", exprType, test.Type)
				return
			}

			bag := ilt.NewFakeBag(test.I)
			r, err := compiled.Evaluate(bag)
			if e := test.CheckEvaluationResult(r, err); e != nil {
				tt.Fatalf(e.Error())
				return
			}

			if !test.CheckReferenced(bag) {
				tt.Fatalf("Referenced attribute mismatch: '%s' != '%s'", bag.ReferencedList(), test.Referenced)
				return
			}

			// Depending on the type, try testing specialized methods as well.
			if test.CompileErr != "" {
				return
			}

			switch exprType {
			case istio_mixer_v1_config_descriptor.BOOL:
				actual, err := compiled.EvaluateBoolean(bag)
				if e := test.CheckEvaluationResult(actual, err); e != nil {
					tt.Fatalf(e.Error())
					return
				}

			case istio_mixer_v1_config_descriptor.STRING,
				istio_mixer_v1_config_descriptor.URI,
				istio_mixer_v1_config_descriptor.EMAIL_ADDRESS,
				istio_mixer_v1_config_descriptor.DNS_NAME:

				actual, err := compiled.EvaluateString(bag)
				if e := test.CheckEvaluationResult(actual, err); e != nil {
					tt.Fatalf(e.Error())
					return
				}

			case istio_mixer_v1_config_descriptor.DOUBLE:
				actual, err := compiled.EvaluateDouble(bag)
				if e := test.CheckEvaluationResult(actual, err); e != nil {
					tt.Fatalf(e.Error())
					return
				}

			case istio_mixer_v1_config_descriptor.INT64:
				actual, err := compiled.EvaluateInteger(bag)
				if e := test.CheckEvaluationResult(actual, err); e != nil {
					tt.Fatalf(e.Error())
					return
				}
			}
		})
	}
}
