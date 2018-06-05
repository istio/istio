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

// NoGoroutineRule defines rule for NoGoroutine.
type NoGoroutineRule struct{}

func newNoGoroutineRule() *NoGoroutineRule {
	return &NoGoroutineRule{}
}

// OnlyCheckTestFunc returns true as SkipByIssueRule only applies to test function with prefix Test.
func (lr *NoGoroutineRule) OnlyCheckTestFunc() bool {
	return false
}

// GetID returns NoGoroutine.
func (lr *NoGoroutineRule) GetID() string {
	return NoGoroutine
}

// Check returns true if aNode is not goroutine, or false otherwise.
func (lr *NoGoroutineRule) Check(aNode ast.Node, fs *token.FileSet) (bool, string) {
	if gs, ok := aNode.(*ast.GoStmt); ok {
		return false, lr.createLintReport(gs.Pos(), fs)
	}

	return true, ""
}

// CreateLintReport returns a message reporting invalid skip call at pos.
func (lr *NoGoroutineRule) createLintReport(pos token.Pos, fs *token.FileSet) string {
	return fmt.Sprintf("%v:%v:%v:%s",
		fs.Position(pos).Filename,
		fs.Position(pos).Line,
		fs.Position(pos).Column,
		"goroutine is disallowed.")
}
