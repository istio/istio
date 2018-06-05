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

// LintReports stores a group of lint processing reports.
type LintReports []string

// Linter applies linting rules to a file.
type Linter struct {
	fpath   string                 // file path that takes linting process
	lreport LintReports            // report for linting process
	tType   TestType               // test type of file path
	fs      *token.FileSet
}

func newLinter(fileP string, t TestType) Linter {
	return Linter{
		fpath: fileP,
		tType: t,
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
		lt.lreport = append(lt.lreport, fmt.Sprintf("%v", err))
		return
	}
	// Walk through the files
	ast.Walk(lt, astFile)

	// Collect all test functions with prefix "Test".
	testFuncs := []*ast.FuncDecl{}
	for _, d := range astFile.Decls {
		if fn, isFn := d.(*ast.FuncDecl); isFn && strings.HasPrefix(fn.Name.Name, "Test") {
			testFuncs = append(testFuncs, fn)
		}
	}
	// Checks each test function with prefix "Test".
	for _, function := range testFuncs {
		if lt.tType == UnitTest {
			lt.ApplyRules(function, UnitTestRules, true)
		} else if lt.tType == IntegTest {
			lt.ApplyRules(function, IntegTestRules, true)
		} else if lt.tType == E2eTest {
			lt.ApplyRules(function, E2eTestRules, true)
		}
	}
}

// ApplyRulesToFile applies rules to node and generate lint report.
func (lt *Linter) ApplyRules(node ast.Node, rules []LintRule, testFunc bool) {
}

// Visit checks each function call and report if a forbidden function call is detected.
func (lt *Linter) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}

	if lt.tType == UnitTest {
		lt.ApplyRules(node, UnitTestRules, false)
	} else if lt.tType == IntegTest {
		lt.ApplyRules(node, IntegTestRules, false)
	} else if lt.tType == E2eTest {
		lt.ApplyRules(node, E2eTestRules, false)
	}
	return lt
}
