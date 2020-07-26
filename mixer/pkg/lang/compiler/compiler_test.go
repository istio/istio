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

package compiler

import (
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/mixer/pkg/il"
	"istio.io/istio/mixer/pkg/il/interpreter"
	ilt "istio.io/istio/mixer/pkg/il/testing"
	"istio.io/istio/mixer/pkg/il/text"
	"istio.io/istio/mixer/pkg/lang"
	"istio.io/istio/mixer/pkg/lang/ast"
	"istio.io/pkg/attribute"
)

func TestCompiler_SingleExpressionSession(t *testing.T) {
	for _, test := range ilt.TestData {
		// If there is no expression in the test, skip it. It is most likely an interpreter test that directly runs
		// off IL.
		if test.E == "" {
			continue
		}

		t.Run(test.TestName(), func(tt *testing.T) {

			finder := attribute.NewFinder(test.Conf())

			fns := lang.ExternFunctionMetadata
			if test.Fns != nil {
				fns = append(fns, test.Fns...)
			}
			compiler := New(finder, ast.FuncMap(fns))
			fnID, _, err := compiler.CompileExpression(test.E)

			if err != nil {
				if err.Error() != test.CompileErr {
					tt.Fatalf("Unexpected error: '%s' != '%s'", err.Error(), test.CompileErr)
				}
				return
			}

			if test.CompileErr != "" {
				tt.Fatalf("expected error not found: '%s'", test.CompileErr)
				return
			}

			if test.IL != "" {
				actual := text.WriteText(compiler.Program())
				// TODO: Expected IL is written with the original Compile code in mind. Do a little bit of hackery
				// to calculate the expected name. Hopefully we can fix this once we get rid of the compile method.
				actual = strings.Replace(actual, "$expression0", "eval", 1)

				if strings.TrimSpace(actual) != strings.TrimSpace(test.IL) {
					tt.Log("===== EXPECTED ====\n")
					tt.Log(test.IL)
					tt.Log("\n====== ACTUAL =====\n")
					tt.Log(actual)
					tt.Log("===================\n")
					tt.Fail()
					return
				}
			}

			// Also perform evaluation
			if e := doEval(test, compiler.program, fnID); e != nil {
				t.Errorf(e.Error())
				return
			}
		})
	}
}

func TestCompiler_DoubleExpressionSession(t *testing.T) {
	for _, test := range ilt.TestData {
		// If there is no expression in the test, skip it. It is most likely an interpreter test that directly runs
		// off IL.
		if test.E == "" {
			continue
		}

		t.Run(test.TestName(), func(tt *testing.T) {

			finder := attribute.NewFinder(test.Conf())

			fns := lang.ExternFunctionMetadata
			if test.Fns != nil {
				fns = append(fns, test.Fns...)
			}
			compiler := New(finder, ast.FuncMap(fns))
			fnID1, _, err := compiler.CompileExpression(test.E)
			if err != nil {
				if err.Error() != test.CompileErr {
					tt.Fatalf("Unexpected error: '%s' != '%s'", err.Error(), test.CompileErr)
				}
				return
			}

			if test.CompileErr != "" {
				tt.Fatalf("expected error not found: '%s'", test.CompileErr)
				return
			}

			// Compile again
			fnID2, _, err := compiler.CompileExpression(test.E)
			if err != nil {
				tt.Fatalf("Unexpected compile error: '%s'", err.Error())
			}

			// Evaluate fn1
			if e := doEval(test, compiler.program, fnID1); e != nil {
				t.Errorf(e.Error())
				return
			}

			// Evaluate fn2
			if e := doEval(test, compiler.program, fnID2); e != nil {
				t.Errorf(e.Error())
				return
			}
		})
	}
}

func doEval(test ilt.TestInfo, p *il.Program, fnID uint32) error {
	b := ilt.NewFakeBag(test.I)

	externs := make(map[string]interpreter.Extern)
	for k, v := range lang.Externs {
		externs[k] = v
	}
	if test.Externs != nil {
		for k, v := range test.Externs {
			externs[k] = interpreter.ExternFromFn(k, v)
		}
	}

	i := interpreter.New(p, externs)
	v, err := i.EvalFnID(fnID, b)
	return test.CheckEvaluationResult(v.AsInterface(), err)
}

func TestCompile(t *testing.T) {

	for i, test := range ilt.TestData {
		// If there is no expression in the test, skip it. It is most likely an interpreter test that directly runs
		// off IL.
		if test.E == "" {
			continue
		}

		name := fmt.Sprintf("%d '%s'", i, test.TestName())
		t.Run(name, func(tt *testing.T) {

			finder := attribute.NewFinder(test.Conf())

			fns := lang.ExternFunctionMetadata
			if test.Fns != nil {
				fns = append(fns, test.Fns...)
			}
			program, err := compile(test.E, finder, ast.FuncMap(fns))
			if err != nil {
				if err.Error() != test.CompileErr {
					tt.Fatalf("Unexpected error: '%s' != '%s'", err.Error(), test.CompileErr)
				}
				return
			}

			if test.CompileErr != "" {
				tt.Fatalf("expected error not found: '%s'", test.CompileErr)
				return
			}

			if test.IL != "" {
				actual := text.WriteText(program)
				if strings.TrimSpace(actual) != strings.TrimSpace(test.IL) {
					tt.Log("===== EXPECTED ====\n")
					tt.Log(test.IL)
					tt.Log("\n====== ACTUAL =====\n")
					tt.Log(actual)
					tt.Log("===================\n")
					tt.Fail()
					return
				}
			}

			input := test.I
			if input == nil {
				input = map[string]interface{}{}
			}
			b := ilt.NewFakeBag(input)

			externs := make(map[string]interpreter.Extern)
			for k, v := range lang.Externs {
				externs[k] = v
			}
			if test.Externs != nil {
				for k, v := range test.Externs {
					externs[k] = interpreter.ExternFromFn(k, v)
				}
			}

			i := interpreter.New(program, externs)
			v, err := i.Eval("eval", b)
			if err != nil {
				if !strings.HasPrefix(err.Error(), test.Err) {
					tt.Fatalf("expected error not found: E:'%v', A:'%v'", test.Err, err)
				}
				return
			}
			if test.Err != "" {
				tt.Fatalf("expected error not received: '%v'", test.Err)
			}

			if !attribute.Equal(test.R, v.AsInterface()) {
				tt.Fatalf("Result match failed: %+v == %+v", test.R, v.AsInterface())
			}
		})
	}
}
