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

package expr

import (
	"errors"
	"strings"
	"testing"

	"istio.io/mixer/pkg/attribute"
)

// run micro benchmark using expression evaluator.
// Results vs hand coded expression eval function
// 2017-03-10
/*
$ go test -bench=.
BenchmarkExpression/ok_constantAST-8         	500000000	         3.70 ns/op
BenchmarkExpression/ok_constantDirect-8      	1000000000	         2.43 ns/op
BenchmarkExpression/ok_1stAST-8              	 1000000	      1626 ns/op
BenchmarkExpression/ok_1stDirect-8           	100000000	        19.9 ns/op
BenchmarkExpression/ok_2ndAST-8              	 1000000	      1623 ns/op
BenchmarkExpression/ok_2ndDirect-8           	30000000	        41.5 ns/op
BenchmarkExpression/ok_notfoundAST-8         	 1000000	      1628 ns/op
BenchmarkExpression/ok_notfoundDirect-8      	30000000	        38.7 ns/op
*/

type exprFunc func(a attribute.Bag) (bool, error)

// a == 20 || request.header["host"] == 50

func BenchmarkExpression(b *testing.B) {
	success := "_SUCCESS_"
	exprStr := `a == 20 || request.header["host"] == "abc"`
	dff := func(a attribute.Bag) (bool, error) {
		var aa int64
		var b bool
		var s map[string]string
		if aa, b = a.Int64("a"); !b {
			return false, errors.New("a not found")
		}
		if aa == 20 {
			return true, nil
		}

		s, b = a.StringMap("request.header")
		if !b {
			return false, errors.New("a not found")
		}
		ss := s["host"]
		if ss == "abc" {
			return true, nil
		}
		return false, nil
	}
	exf, _ := Parse(exprStr)

	exprConstant := "true"
	ex2, _ := Parse(exprConstant)
	df2 := func(a attribute.Bag) (bool, error) { return true, nil }

	fm := FuncMap()
	tests := []struct {
		name   string
		tmap   map[string]interface{}
		result bool
		err    string
		df     exprFunc
		ex     *Expression
	}{
		{"ok_constant", map[string]interface{}{
			"a": int64(20),
			"request.header": map[string]string{
				"host": "abc",
			},
		}, true, success, df2, ex2,
		},
		{"ok_1st", map[string]interface{}{
			"a": int64(20),
			"request.header": map[string]string{
				"host": "abc",
			},
		}, true, success, dff, exf,
		},
		{"ok_2nd", map[string]interface{}{
			"a": int64(2),
			"request.header": map[string]string{
				"host": "abc",
			},
		}, true, success, dff, exf,
		},
		{"ok_notfound", map[string]interface{}{
			"a": int64(2),
			"request.header": map[string]string{
				"host": "abcd",
			},
		}, false, success, dff, exf,
		},
	}

	//assertNoError
	assertNoError := func(err error, errStr string, ret interface{}, want bool, t *testing.B) {
		if (err == nil) != (errStr == success) {
			t.Errorf("got %s, want %s", err, errStr)
		}
		// check if error is of the correct type
		if err != nil {
			if !strings.Contains(err.Error(), errStr) {
				t.Errorf("got %s, want %s", err, errStr)
			}
			return
		}
		// check result
		if ret != want {
			t.Errorf("got %v, want %v", ret, want)
		}
	}

	for _, tst := range tests {
		attrs := &bag{attrs: tst.tmap}
		ii, err := tst.ex.Eval(attrs, fm)
		assertNoError(err, tst.err, ii, tst.result, b)
		b.Run(tst.name+"AST", func(bb *testing.B) {
			for n := 0; n < bb.N; n++ {
				_, _ = tst.ex.Eval(attrs, fm)
			}
		})

		ii, err = tst.df(attrs)
		assertNoError(err, tst.err, ii, tst.result, b)
		b.Run(tst.name+"Direct", func(bb *testing.B) {
			for n := 0; n < bb.N; n++ {
				_, _ = tst.df(attrs)
			}
		})
	}
}
