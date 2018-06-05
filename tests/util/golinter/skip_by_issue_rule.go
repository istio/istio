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

type SkipByIssueRule struct {
	skipArgsRegex string // Defines arg in t.Skip() that should match.
}

func newSkipByIssueRule() *SkipByIssueRule {
	return &SkipByIssueRule{
		skipArgsRegex: `https:\/\/github\.com\/istio\/istio\/issues\/[0-9]+`,
}
}

// GetID returns ID SkipByIssue.
func (lr *SkipByIssueRule) GetID() RuleID {
	return SkipByIssue
}

// OnlyCheckTestFunc returns true as SkipByIssueRule only applies to test function with prefix Test.
func (lr *SkipByIssueRule) OnlyCheckTestFunc() bool	{
	return true
}

// GetName returns SkipByIssue.
func (lr *SkipByIssueRule) GetName() string	{
	return "SkipByIssue"
}

// Check returns true if aNode is a valid t.Skip(), or aNode is not t.Skip(), t.SkipNow(),
// and t.Skipf().
// This is an example for valid call t.Skip("https://github.com/istio/istio/issues/6012")
// These calls are not valid:
// t.Skip("https://istio.io/"),
// t.SkipNow(),
// t.Skipf("https://istio.io/%d", x).
func (lr *SkipByIssueRule) Check(aNode ast.Node, fs *token.FileSet) (bool, string)	{
	if fn, isFn := aNode.(*ast.FuncDecl); isFn {
		for _, bd := range fn.Body.List {
			if ok, _ := matchFunc(bd, "t", "SkipNow"); ok {
				return false, lr.createLintReport(bd.Pos(), fs)
			} else if ok, _ := matchFunc(bd, "t", "Skipf"); ok {
				return false, lr.createLintReport(bd.Pos(), fs)
			} else if ok, fcall := matchFunc(bd, "t", "Skip");
			ok && !matchFuncArgs(fcall, lr.skipArgsRegex) {
				return false, lr.createLintReport(bd.Pos(), fs)
			}
		}
	}
	return true, "";
}

// CreateLintReport returns a message reporting invalid skip call at pos.
func (lr *SkipByIssueRule) createLintReport(pos token.Pos, fs *token.FileSet) string {
	return fmt.Sprintf("%v:%v:%v:%s",
		fs.Position(pos).Filename,
		fs.Position(pos).Line,
		fs.Position(pos).Column,
		"Only t.Skip() is allowed and t.Skip() should contain an url to GitHub issue.")
}