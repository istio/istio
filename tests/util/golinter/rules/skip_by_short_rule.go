// Copyright 2018 Istio Authors. All Rights Reserved.
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

package rules

import (
	"go/ast"
	"go/token"
	"strings"
	"istio.io/istio/tests/util/golinter/linter"
)

// SkipByShortRule requires that a test function should have one of these pattern.
// Pattern 1
// func TestA(t *testing.T) {
//   if !testing.Short() {
//    ...
//   }
// }
//
// Pattern 2
// func TestB(t *testing.T) {
//   if testing.Short() {
//     t.Skip("xxx")
//   }
//   ...
// }
type SkipByShortRule struct{}

func NewSkipByShortRule() *SkipByShortRule {
	return &SkipByShortRule{}
}

// GetID returns SkipByShort.
func (lr *SkipByShortRule) GetID() string {
	return SkipByShort
}

// Check verifies if aNode is a valid t.Skip(). If verification fails it reports to linter.
// There are two examples for valid t.Skip().
// case 1:
// func Testxxx(t *testing.T) {
// 	if !testing.Short() {
// 	...
// 	}
// }
// case 2:
// func Testxxx(t *testing.T) {
// 	if testing.Short() {
//		t.Skip("xxx")
//	}
//	...
// }
func (lr *SkipByShortRule) Check(aNode ast.Node, lt *linter.Linter) {
	if fn, isFn := aNode.(*ast.FuncDecl); isFn && strings.HasPrefix(fn.Name.Name, "Test") {
		if len(fn.Body.List) == 0 {
			rpt := createLintReport(aNode.Pos(), lt.Fs(), "Missing either 'if testing.Short() { t.Skip() }' or 'if !testing.Short() {}'")
			lt.LReport() = append(lt.LReport(), rpt)
		} else if len(fn.Body.List) == 1 {
			if ifStmt, ok := fn.Body.List[0].(*ast.IfStmt); ok {
				if uExpr, ok := ifStmt.Cond.(*ast.UnaryExpr); ok {
					if call, ok := uExpr.X.(*ast.CallExpr); ok && uExpr.Op == token.NOT {
						if matchCallExpr(call, "testing", "Short") {
							return
						}
					}
				}
			}
		} else {
			if ifStmt, ok := fn.Body.List[0].(*ast.IfStmt); ok {
				if call, ok := ifStmt.Cond.(*ast.CallExpr); ok {
					if matchCallExpr(call, "testing", "Short") && len(ifStmt.Body.List) > 0 {
						if exprStmt, ok := ifStmt.Body.List[0].(*ast.ExprStmt); ok {
							if call, ok := exprStmt.X.(*ast.CallExpr); ok {
								if matchCallExpr(call, "t", "Skip") {
									return
								}
							}
						}
					}
				}
			}
		}
		rpt := createLintReport(aNode.Pos(), lt.Fs(), "Missing either 'if testing.Short() { t.Skip() }' or 'if !testing.Short() {}'")
		lt.LReport() = append(lt.LReport(), rpt)
	}
}
