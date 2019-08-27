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
	"log"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// GetCallerFileName returns filename of caller without file extension.
func GetCallerFileName() string {
	if _, filename, _, ok := runtime.Caller(1); ok {
		fnBase := filepath.Base(filename)
		fn := strings.Split(fnBase, ".")
		if len(fn) > 0 {
			return fn[0]
		}
	} else {
		log.Print("Unable to get filename for caller.")
	}
	return ""
}

// MatchCallExpr returns true if ce matches package name pn and method name mn.
func MatchCallExpr(ce *ast.CallExpr, pn string, mn string) bool {
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

// matchFunc returns true if bd matches packageName and methodName, and returns CallExpr
// element in bd for args check.
func matchFunc(bd ast.Stmt, packageName string, methodName string) (bool, *ast.CallExpr) {
	if exprStmt, ok := bd.(*ast.ExprStmt); ok {
		if call, ok := exprStmt.X.(*ast.CallExpr); ok {
			if fun, ok := call.Fun.(*ast.SelectorExpr); ok {
				if astid, ok := fun.X.(*ast.Ident); ok {
					if astid.String() == packageName && fun.Sel.String() == methodName {
						return true, call
					}
				}
			}
		}
	}
	return false, nil
}
