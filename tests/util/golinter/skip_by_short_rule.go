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

package main

import (
	"fmt"
	"go/ast"
	"go/token"
)

// SkipByShortRule defines rule for SkipByShort
type SkipByShortRule struct{}

func newSkipByShortRule() *SkipByShortRule {
	return &SkipByShortRule{}
}

// OnlyCheckTestFunc returns true as SkipByIssueRule only applies to test function with prefix Test.
func (lr *SkipByShortRule) OnlyCheckTestFunc() bool {
	return true
}

// GetID returns SkipByShort.
func (lr *SkipByShortRule) GetID() string {
	return SkipByShort
}

// Check returns true if aNode is a valid t.Skip(). Otherwise, returns false.
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
func (lr *SkipByShortRule) Check(aNode ast.Node, fs *token.FileSet) (bool, string) {
	if fn, isFn := aNode.(*ast.FuncDecl); isFn {
		if len(fn.Body.List) == 0 {
			return false, lr.createLintReport(aNode.Pos(), fs)
		} else if len(fn.Body.List) == 1 {
			if ifStmt, ok := fn.Body.List[0].(*ast.IfStmt); ok {
				if uExpr, ok := ifStmt.Cond.(*ast.UnaryExpr); ok {
					if call, ok := uExpr.X.(*ast.CallExpr); ok && uExpr.Op == token.NOT {
						if matchCallExpr(call, "testing", "Short") {
							return true, ""
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
									return true, ""
								}
							}
						}
					}
				}
			}
		}
	}
	return false, lr.createLintReport(aNode.Pos(), fs)
}

// CreateLintReport returns a message reporting invalid skip call at pos that violates rule rn.
func (lr *SkipByShortRule) createLintReport(pos token.Pos, fs *token.FileSet) string {
	return fmt.Sprintf("%v:%v:%v:%s",
		fs.Position(pos).Filename,
		fs.Position(pos).Line,
		fs.Position(pos).Column,
		"Missing either 'if testing.Short() { t.Skip() }' or 'if !testing.Short() {}'")
}
