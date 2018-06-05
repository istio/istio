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

type NoSleepRule struct {}

func newNoSleepRule() *NoSleepRule {
	return &NoSleepRule{}
}

// OnlyCheckTestFunc returns false as NoSleepRuleRule applies to whole file.
func (lr *NoSleepRule) OnlyCheckTestFunc() bool	{
	return false
}

// GetID returns NoSleep.
func (lr *NoSleepRule) GetID() string	{
	return NoSleep
}

// Check returns true if aNode is not time.Sleep. Otherwise it returns false and error report.
func (lr *NoSleepRule) Check(aNode ast.Node, fs *token.FileSet) (bool, string)	{
	if ce, ok := aNode.(*ast.CallExpr); ok {
		if matchCallExpr(ce, "time", "Sleep") {
			return false, lr.createLintReport(ce.Pos(), fs)
		}
	}
	return true, ""
}

// CreateLintReport returns a message reporting invalid skip call at pos.
func (lr *NoSleepRule) createLintReport(pos token.Pos, fs *token.FileSet) string {
	return fmt.Sprintf("%v:%v:%v:%s",
		fs.Position(pos).Filename,
		fs.Position(pos).Line,
		fs.Position(pos).Column,
		"time.Sleep() is disallowed.")
}
