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

package checker

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

var (
	// IgnoreTestLinterData skips over unit tests
	IgnoreTestLinterData = true
)

// Check checks the list of files, and write to the given Report.
func Check(paths []string, factory RulesFactory, whitelist *Whitelist, report *Report) error {
	// Empty paths means current dir.
	if len(paths) == 0 {
		paths = []string{"."}
	}

	for _, path := range paths {
		if !filepath.IsAbs(path) {
			path, _ = filepath.Abs(path)
		}
		err := filepath.Walk(path, func(fpath string, info os.FileInfo, err error) error {
			if err != nil {
				return fmt.Errorf("pervent panic by handling failure accessing a path %q: %v", fpath, err)
			}
			rules := factory.GetRules(fpath, info)
			if len(rules) > 0 {
				fileCheck(fpath, rules, whitelist, report)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("error visiting the path %q: %v", path, err)
		}
	}
	return nil
}

// fileCheck checks a file using the given rules, and write to the given Report.
func fileCheck(path string, rules []Rule, whitelist *Whitelist, report *Report) {
	// TODO: skip over linter tests in a principled manner for all linters
	if IgnoreTestLinterData && strings.Contains(path, "testlinter/testdata") {
		return
	}

	fs := token.NewFileSet()
	astFile, err := parser.ParseFile(fs, path, nil, parser.Mode(0))
	if err != nil {
		report.AddString(fmt.Sprintf("%v", err))
		return
	}
	v := FileVisitor{
		path:      path,
		rules:     rules,
		whitelist: whitelist,
		fileset:   fs,
		report:    report,
	}
	// Walk through the files
	ast.Walk(&v, astFile)
}

// FileVisitor visits the go file syntax tree and applies the given rules.
type FileVisitor struct {
	path      string
	rules     []Rule     // rules to check
	whitelist *Whitelist // rules to skip
	fileset   *token.FileSet
	report    *Report // report for linting process
}

// Visit checks each node and runs the applicable checks.
func (fv *FileVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}

	// ApplyRules applies rules to node and generate lint report.
	for _, rule := range fv.rules {
		if !fv.whitelist.Apply(fv.path, rule) {
			rule.Check(node, fv.fileset, fv.report)
		}
	}
	return fv
}
