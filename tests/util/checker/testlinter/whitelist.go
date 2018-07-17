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
	"regexp"
	"strings"

	"istio.io/istio/tests/util/checker"
)

// Whitelist determines if rules are whitelisted for the given functions.
type Whitelist struct {
	// Map from functions to whitelisted rules.
	RuleWhitelist map[string][]string
}

// Apply returns true if the given rule is whitelisted for a function.
func (wl *Whitelist) Apply(funcName string, rule checker.Rule) bool {
	if _, hasFunc := wl.RuleWhitelist[funcName]; !hasFunc {
		return false
	}
	for _, skipRule := range wl.RuleWhitelist[funcName] {
		if skipRule == rule.GetID() || skipRule == "*" {
			return true
		}
	}
	return false
}

// WhitelistParser extracts whitelisted rules from comments in a file.
type WhitelistParser struct {
}

// GetWhitelist extract whitelist rules at function level and file level, and
// returns a Whitelist object storing the information.
func (wp *WhitelistParser) GetWhitelist(astFile *ast.File) checker.Whitelist {
	wl := Whitelist{RuleWhitelist: map[string][]string{}}

	// Extract function level whitelist rules.
	for _, d := range astFile.Decls {
		if fn, isFn := d.(*ast.FuncDecl); isFn && fn.Name.String() != "TestMain" {
			wp.getWhitelistFromFunctionComment(fn.Name.String(), fn.Doc.Text(), &wl)
		}
	}

	wp.getWhitelistFromGlobalComment(astFile, &wl)
	return &wl
}

// patternMatcher extracts whitelist rules from comment and returns rules.
func (wp *WhitelistParser) patternMatcher(comment string) []string {
	whitelistPattern := regexp.MustCompile(`whitelist\(https:\/\/github\.com\/istio\/istio\/issues\/[0-9]+\):([^}]*)\.`)
	matched := whitelistPattern.FindStringSubmatch(comment)
	if len(matched) > 1 {
		splitByPeriod := strings.Split(matched[1], ".")
		rules := strings.Split(splitByPeriod[0], ",")
		for i, rule := range rules {
			ruleIDStr := strings.TrimSpace(rule)
			if ruleIDStr == "*" {
				return []string{"*"}
			}
			rules[i] = ruleIDStr
		}
		return rules
	}
	return []string{}
}

// getWhitelistFromFunctionComment extracts whitelist rule IDs from function
// comment and fills into Whitelist. Comment should match this pattern, and ends
// with a period. whitelist(url-to-GitHub-issue):[rule ID 1],[rule ID 2]...
// For example:
// whitelist(https://github.com/istio/istio/issues/6346):no_sleep,skip_issue,short_skip.
// whitelist(https://github.com/istio/istio/issues/6346):skip_allrules.
func (wp *WhitelistParser) getWhitelistFromFunctionComment(funcName, comment string, wl *Whitelist) {
	wl.RuleWhitelist[funcName] = wp.patternMatcher(comment)
}

// getWhitelistFromGlobalComment extracts whitelist rules that apply to the entire file.
// Whitelist rules should be defined in a comment pattern which ends with a period:
// whitelist(url-to-GitHub-issue):[rule ID 1],[rule ID 2]...
// The whitelist pattern should be placed in comments of TestMain function.
// e.g.
// // whitelist(https://github.com/istio/istio/issues/6346):no_sleep,skip_issue,short_skip.
// func TestMain(m *testing.M) {}
// TODO(JimmyCYJ): find a way to define file level whitelist without TestMain() comment.
func (wp *WhitelistParser) getWhitelistFromGlobalComment(astFile *ast.File, wl *Whitelist) {
	// check comments of TestMain()
	for _, d := range astFile.Decls {
		if fn, isFn := d.(*ast.FuncDecl); isFn && fn.Name.String() == "TestMain" {
			tmRules := wp.patternMatcher(fn.Doc.Text())
			if len(tmRules) > 0 && tmRules[0] == "*" {
				for fName := range wl.RuleWhitelist {
					wl.RuleWhitelist[fName] = tmRules
				}
			} else {
				for fName, list := range wl.RuleWhitelist {
					wl.RuleWhitelist[fName] = append(list, tmRules...)
				}
			}
		}
	}
}
