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
	"testing"

	ilt "istio.io/istio/mixer/pkg/il/testing"
	"istio.io/istio/mixer/pkg/lang"
	"istio.io/istio/mixer/pkg/lang/ast"
)

func TestCompiler_SingleExpressionSession(t *testing.T) {
	t.Skip()
	for _, test := range ilt.TestData {
		// If there is no expression in the test, skip it. It is most likely an interpreter test that directly runs
		// off IL.
		if test.E == "" {
			continue
		}

		t.Run(test.TestName(), func(tt *testing.T) {

			finder := ast.NewFinder(test.Conf())

			fns := lang.ExternFunctionMetadata
			if test.Fns != nil {
				fns = append(fns, test.Fns...)
			}
			_ = fns

			// Also perform evaluation
			_ = finder
		})
	}
}

/*
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
*/
