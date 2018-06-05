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
	"go/ast"
	"go/token"
	"regexp"
)

// Defines ID for each lint rule. Each rule is assigned a unique ID. AllRules is a special name
// used only in whitelist, which skips all rules for a file path.
const (
	SkipByIssue = "SkipByIssue" // SkipByIssue == 0
	NoSleep     = "NoSleep" // DoNotSleep == 1
	NoGoroutine = "NoGoroutine" // NoGoroutine == 2
	SkipByShort = "SkipByShort" // SkipByShort == 3

	// Please leave AllRules to be the last one.
	AllRules = "AllRules" // AllRules == 4
)

type LintRule interface {
	// GetID returns ID of the rule in string.
	GetID() string
	// OnlyCheckTestFunc returns true if lint rule only applies to test function with prefix Test.
	OnlyCheckTestFunc() bool
	// Check returns true if aNode passes rule check, or false with no-empty error report if check fails.
	Check(aNode ast.Node, fs *token.FileSet) (bool, string)
	// createLintReport returns lint error report for function call at position pos.
	createLintReport(pos token.Pos, fs *token.FileSet) string
}

// matchFunc returns true if bd matches package name pn and method name mn, and returns CallExpr
// element in bd for args check.
func matchFunc(bd ast.Stmt, pn string, mn string) (bool, *ast.CallExpr) {
	if exprStmt, ok := bd.(*ast.ExprStmt); ok {
		if call, ok := exprStmt.X.(*ast.CallExpr); ok {
			if fun, ok := call.Fun.(*ast.SelectorExpr); ok {
				if astid, ok := fun.X.(*ast.Ident); ok {
					if astid.String() == pn && fun.Sel.String() == mn {
						return true, call
					}
				}
			}
		}
	}
	return false, nil
}

// matchCallExpr returns true if ce matches package name pn and method name mn.
func matchCallExpr(ce *ast.CallExpr, pn string, mn string) bool {
	if sel, ok := ce.Fun.(*ast.SelectorExpr); ok {
		if pkg, ok := sel.X.(*ast.Ident); ok {
			return pkg.String() == pn && sel.Sel.String() == mn
		}
	}
	return false
}

// matchFuncArgs returns true if args in fcall matches argsR. argsR is regex.
func matchFuncArgs(fcall *ast.CallExpr, argsR string) bool {
	if len(fcall.Args) == 1 && len(argsR) > 0 {
		if blit, ok := fcall.Args[0].(*ast.BasicLit); ok {
			if blit.Kind == token.STRING {
				matched, _ := regexp.MatchString(argsR, blit.Value)
				return matched
			}
		}
	}
	return false
}
