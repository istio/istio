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

package linter

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"

	"istio.io/istio/tests/util/golinter/rules"
)

// Linter applies linting rules to a file.
type Linter struct {
	fpath    string   // file path that takes linting process
	lreport  []string // report for linting process
	tType    TestType // test type of file path
	fs       *token.FileSet
	sRuleMap map[string]bool // map of rules to be skipped.
}

// NewLinter creates a new Linter object and returns the object.
func NewLinter(fileP string, t TestType, rMap map[string]bool) Linter {
	return Linter{
		fpath:    fileP,
		tType:    t,
		fs:       token.NewFileSet(),
		sRuleMap: rMap,
	}
}

// LReport returns report from Linter
func (lt *Linter) LReport() []string {
	return lt.lreport
}

// Fs returns fs from Linter
func (lt *Linter) Fs() *token.FileSet {
	return lt.fs
}

// Run applies linting rule to file in fpath.
func (lt *Linter) Run() {
	if _, skipAll := lt.sRuleMap[SkipAllRules]; skipAll {
		return
	}

	astFile, err := parser.ParseFile(lt.Fs(), lt.fpath, nil, parser.Mode(0))
	if err != nil {
		lt.lreport = append(lt.lreport, fmt.Sprintf("%v", err))
		return
	}
	// Walk through the files
	ast.Walk(lt, astFile)
}

// ApplyRules applies rules to node and generate lint report.
func (lt *Linter) ApplyRules(node ast.Node, lintrules []rules.LintRule) {
	for _, rule := range lintrules {
		if _, skip := lt.sRuleMap[rule.GetID()]; !skip {
			reporter := rules.NewLintReport(&lt.lreport)
			rule.Check(node, lt.fs, reporter)
		}
	}
}

// Visit checks each function call and report if a forbidden function call is detected.
func (lt *Linter) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}

	if lt.tType != NonTest {
		lt.ApplyRules(node, LintRulesList[lt.tType])
	}
	return lt
}
