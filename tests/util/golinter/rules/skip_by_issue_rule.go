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
)

// SkipByIssueRule requires that a `t.Skip()` call in test function should contain url to a issue.
// This helps to keep tracking of the issue that causes a test to be skipped.
// For example, this is a valid call,
// t.Skip("https://github.com/istio/istio/issues/6012")
// t.SkipNow() and t.Skipf() are not allowed.
type SkipByIssueRule struct {
	skipArgsRegex string // Defines arg in t.Skip() that should match.
}

func NewSkipByIssueRule() *SkipByIssueRule {
	return &SkipByIssueRule{
		skipArgsRegex: `https:\/\/github\.com\/istio\/istio\/issues\/[0-9]+`,
	}
}

// GetID returns skip_by_issue_rule.
func (lr *SkipByIssueRule) GetID() string {
	return getCallerFileName()
}

// Check returns verifies if aNode is a valid t.Skip(), or aNode is not t.Skip(), t.SkipNow(),
// and t.Skipf(). If verification fails it adds report into rpt.
// This is an example for valid call t.Skip("https://github.com/istio/istio/issues/6012")
// These calls are not valid:
// t.Skip("https://istio.io/"),
// t.SkipNow(),
// t.Skipf("https://istio.io/%d", x).
func (lr *SkipByIssueRule) Check(aNode ast.Node, fs *token.FileSet, rpt *[]string) {
	if fn, isFn := aNode.(*ast.FuncDecl); isFn {
		for _, bd := range fn.Body.List {
			if ok, _ := matchFunc(bd, "t", "SkipNow"); ok {
				report := createLintReport(bd.Pos(), fs, "Only t.Skip() is allowed and t.Skip() should contain an url to GitHub issue.")
				*rpt = append(*rpt, report)
			} else if ok, _ := matchFunc(bd, "t", "Skipf"); ok {
				report := createLintReport(bd.Pos(), fs, "Only t.Skip() is allowed and t.Skip() should contain an url to GitHub issue.")
				*rpt = append(*rpt, report)
			} else if ok, fcall := matchFunc(bd, "t", "Skip"); ok && !matchFuncArgs(fcall, lr.skipArgsRegex) {
				report := createLintReport(bd.Pos(), fs, "Only t.Skip() is allowed and t.Skip() should contain an url to GitHub issue.")
				*rpt = append(*rpt, report)
			}
		}
	}
}
