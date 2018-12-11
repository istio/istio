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

package compiler

import (
	"strings"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/lang/ast"
)

// caseInsensitiveStringMap lower-cases the stringmap arguments for a select
// set of variables.
type caseInsensitiveStringMap struct {
	vars map[string]struct{}
}

func newCaseInsensitiveStringMap(vars ...string) *caseInsensitiveStringMap {
	set := make(map[string]struct{}, len(vars))
	for _, v := range vars {
		set[v] = struct{}{}
	}
	return &caseInsensitiveStringMap{
		vars: set,
	}
}

func (ci *caseInsensitiveStringMap) rewrite(e *ast.Expression) {
	// only functions need to be rewritten and recursed into
	if e.Fn == nil {
		return
	}

	if e.Fn.Name == "INDEX" {
		// assume args[0] is the string map and args[1] is the key

		switch {
		case e.Fn.Args[0].Const != nil:
			// rewrite does not apply to constant string maps
		case e.Fn.Args[0].Fn != nil:
			// expressions do not evaluate to variables, so the result of a function is a constant value
		case e.Fn.Args[0].Var != nil:
			// rewrite only if variable matches
			if _, ok := ci.vars[e.Fn.Args[0].Var.Name]; ok {
				switch {
				case e.Fn.Args[1].Const != nil:
					if e.Fn.Args[1].Const.Type == v1beta1.STRING {
						e.Fn.Args[1] = &ast.Expression{
							Const: &ast.Constant{
								Type:  v1beta1.STRING,
								Value: strings.ToLower(e.Fn.Args[1].Const.Value.(string)),
							},
						}
					}

				case e.Fn.Args[1].Fn != nil, e.Fn.Args[1].Var != nil:
					ci.rewrite(e.Fn.Args[1])
					e.Fn.Args[1] = &ast.Expression{
						Fn: &ast.Function{
							Name: "toLower",
							Args: []*ast.Expression{e.Fn.Args[1]},
						},
					}
				}
				return
			}
		}
	}

	if e.Fn.Target != nil {
		ci.rewrite(e.Fn.Target)
	}
	for _, expr := range e.Fn.Args {
		ci.rewrite(expr)
	}
}
