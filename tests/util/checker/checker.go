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
	"regexp"
	"strings"
)

// Check checks the list of files, and write to the given Report.
func Check(paths []string, factory RulesFactory, report *Report) error {
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
				fileCheck(fpath, rules, report)
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
func fileCheck(path string, rules []Rule, report *Report) {
	fs := token.NewFileSet()
	astFile, err := parser.ParseFile(fs, path, nil, parser.ParseComments)
	if err != nil {
		report.AddString(fmt.Sprintf("%v", err))
		return
	}
	v := FileVisitor{
		path:    path,
		rules:   rules,
		fileset: fs,
		report:  report,
	}
	// Walk through the files
	// ast.Walk(&v, astFile)
	for _, d := range astFile.Decls {
		if fn, isFn := d.(*ast.FuncDecl); isFn && strings.HasPrefix(fn.Name.String(), "Test") {
			v.GetWhitelist(fn.Doc.Text())
			ast.Walk(&v, fn)
		}
	}
	v.report.Sort()
}

// FileVisitor visits the go file syntax tree and applies the given rules.
type FileVisitor struct {
	path      string
	rules     []Rule     // rules to check
	whitelist *Whitelist // rules to skip
	fileset   *token.FileSet
	report    *Report // report for linting process
}

// GetWhitelist extracts whitelist rule IDs from comment and fills into FileVistor.
// comment should match this pattern, and ends with a period.
// whitelist(url-to-GitHub-issue):[rule ID 1],[rule ID 2]...
// For example:
// whitelist(https://github.com/istio/istio/issues/6346):no_sleep,skip_issue,short_skip.
// whitelist(https://github.com/istio/istio/issues/6346):skip_allrules.
func (fv *FileVisitor) GetWhitelist(comment string) {
	whitelistPattern := regexp.MustCompile(`whitelist\(https:\/\/github\.com\/istio\/istio\/issues\/[0-9]+\):([^}]*)\.`)
	matched := whitelistPattern.FindStringSubmatch(comment)
	if len(matched) > 1 {
		rules := strings.Split(matched[1], ",")
		for i, rule := range rules {
			rules[i] = strings.TrimSpace(rule)
		}
		fv.whitelist = NewWhitelist(rules)
	} else {
		fv.whitelist = NewWhitelist([]string{})
	}
}

// Visit checks each node and runs the applicable checks.
func (fv *FileVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}

	// ApplyRules applies rules to node and generate lint report.
	for _, rule := range fv.rules {
		if !fv.whitelist.Apply(rule) {
			rule.Check(node, fv.fileset, fv.report)
		}
	}
	return fv
}
