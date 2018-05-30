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
	"go/parser"
	"go/token"
	"log"
	"strings"
)

// LintReport stores report after processing a file.
type LintReport struct {
	pos token.Pos
	msg string
}

// LintReports stores a group of lint processing reports.
type LintReports []LintReport

type forbiddenFunctionList struct {
	fInfos []string
	goRt   bool // Whether to check goroutine
}

func newForbiddenFunctionList() forbiddenFunctionList {
	ffl := forbiddenFunctionList{
		fInfos: forbiddenFunctionCalls,
		goRt:   true,
	}
	return ffl
}

// Linter applies linting rules to a file.
type Linter struct {
	fpath   string                 // file path that takes linting process
	lreport LintReports            // report for linting process
	tType   TestType               // test type of file path
	ffl     *forbiddenFunctionList // pointer pointing to list of forbidden functions
	fs      *token.FileSet
}

func newLinter(fileP string, t TestType, ffnl *forbiddenFunctionList) Linter {
	return Linter{
		fpath: fileP,
		tType: t,
		ffl:   ffnl,
		fs:    token.NewFileSet(),
	}
}

// LReport returns report from Linter
func (lt *Linter) LReport() LintReports {
	return lt.lreport
}

// Fs returns fs from Linter
func (lt *Linter) Fs() *token.FileSet {
	return lt.fs
}

// Run applies linting rule to file in fpath.
func (lt *Linter) Run() {
	astFile, err := parser.ParseFile(lt.Fs(), lt.fpath, nil, parser.Mode(0))
	if err != nil {
		lt.lreport = append(lt.lreport, LintReport{token.NoPos, fmt.Sprintf("%v", err)})
		return
	}
	switch lt.tType {
	case UnitTest:
		lt.scanForbiddenFunctionCallInTest(astFile)
	case IntegTest:
		lt.scanMandatoryFunctionCallInTest(astFile)
	case E2eTest:
		lt.scanMandatoryFunctionCallInTest(astFile)
	default:
		log.Printf("Test type is invalid %d", lt.tType)
	}
}

func (lt *Linter) scanForbiddenFunctionCallInTest(af ast.Node) {
	ast.Walk(lt, af)
}

// Visit checks each function call and report if a forbidden function call is detected.
func (lt *Linter) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}

	ce, ok := node.(*ast.CallExpr)
	if ok {
		// Check function call against forbidden function list and update report.
		for _, finfo := range lt.ffl.fInfos {
			if lt.isForbiddenCall(ce.Fun, finfo) {
				lt.updateLintReport(finfo, ce.Pos(), nil)
			}
		}
	}
	// Check goroutine call.
	gs, ok := node.(*ast.GoStmt)
	if ok && lt.ffl.goRt {
		lt.updateLintReport("goroutine", gs.Pos(), nil)
	}

	return lt
}

// updateLintReport updates report catching a forbidden function call finfo or a missing
// mandatory function finfo.
func (lt *Linter) updateLintReport(finfo string, pos token.Pos, testFunc *ast.FuncDecl) {
	var reportMsg string
	if lt.tType == UnitTest {
		reportMsg = fmt.Sprintf("%v:%v:%v:%s",
			lt.fs.Position(pos).Filename,
			lt.fs.Position(pos).Line,
			lt.fs.Position(pos).Column,
			"invalid "+finfo+"() call in "+TestTypeToString[lt.tType])
	} else {
		reportMsg = fmt.Sprintf("%v:%v:%v:%s %s %s",
			lt.fs.Position(pos).Filename,
			lt.fs.Position(testFunc.Pos()).Line,
			lt.fs.Position(testFunc.Pos()).Column,
			"Missing testing.Short() call and t.Skip() call at the beginning of",
			TestTypeToString[lt.tType], testFunc.Name.String())
	}
	newRpt := LintReport{
		pos,
		reportMsg,
	}
	lt.lreport = append(lt.lreport, newRpt)
}

// isForbiddenCall returns true if expr matches forbidden function fn.
func (lt *Linter) isForbiddenCall(expr ast.Expr, fn string) bool {
	if sel, ok := expr.(*ast.SelectorExpr); ok {
		pkg, pkgok := sel.X.(*ast.Ident)
		if pkgok {
			fcall := pkg.Name + "." + sel.Sel.Name
			return fcall == fn
		}
	}
	return false
}

func (lt *Linter) scanMandatoryFunctionCallInTest(af *ast.File) {
	// Collect all test functions with prefix "Test".
	testFuncs := []*ast.FuncDecl{}
	for _, d := range af.Decls {
		if fn, isFn := d.(*ast.FuncDecl); isFn && strings.HasPrefix(fn.Name.Name, "Test") {
			testFuncs = append(testFuncs, fn)
		}
	}
	// Checks each test function with prefix "Test".
	for _, function := range testFuncs {
		// log.Printf("-- function %s", function.Name.String())
		if !lt.hasMandatoryCall(function.Body.List) {
			lt.updateLintReport("", af.Pos(), function)
		}
	}
}

// hasMandatoryCall examines the mandatory function call in a function of the form TestXxx.
// Currently we check the following calls.
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
func (lt *Linter) hasMandatoryCall(stmts []ast.Stmt) bool {
	if len(stmts) == 0 {
		return false
	} else if len(stmts) == 1 {
		if ifStmt, ok := stmts[0].(*ast.IfStmt); ok {
			if uExpr, ok := ifStmt.Cond.(*ast.UnaryExpr); ok {
				if call, ok := uExpr.X.(*ast.CallExpr); ok && uExpr.Op == token.NOT {
					if fun, ok := call.Fun.(*ast.SelectorExpr); ok {
						if astid, ok := fun.X.(*ast.Ident); ok {
							return astid.String() == "testing" && fun.Sel.String() == "Short"
						}
					}
				}
			}
		}
	} else {
		hasShortAtTop := false
		hasSkipAtTop := false
		if ifStmt, ok := stmts[0].(*ast.IfStmt); ok {
			if call, ok := ifStmt.Cond.(*ast.CallExpr); ok {
				if fun, ok := call.Fun.(*ast.SelectorExpr); ok {
					if astid, ok := fun.X.(*ast.Ident); ok {
						if astid.String() == "testing" && fun.Sel.String() == "Short" {
							hasShortAtTop = true
						}
					}
				}
				if len(ifStmt.Body.List) > 0 {
					if exprStmt, ok := ifStmt.Body.List[0].(*ast.ExprStmt); ok {
						if call, ok := exprStmt.X.(*ast.CallExpr); ok {
							if fun, ok := call.Fun.(*ast.SelectorExpr); ok {
								if astid, ok := fun.X.(*ast.Ident); ok {
									if astid.String() == "t" && fun.Sel.String() == "Skip" {
										hasSkipAtTop = true
									}
								}
							}
						}
					}
				}
			}
		}
		return hasShortAtTop && hasSkipAtTop
	}
	return false
}
