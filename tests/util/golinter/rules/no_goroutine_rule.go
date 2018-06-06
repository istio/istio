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

// NoGoroutineRule requires that go f(x, y, z) is not allowed.
type NoGoroutineRule struct{}

func NewNoGoroutineRule() *NoGoroutineRule {
	return &NoGoroutineRule{}
}

// GetID returns no_goroutine_rule.
func (lr *NoGoroutineRule) GetID() string {
	return getCallerFileName()
}

// Check verifies if aNode is not goroutine. If verification fails it adds report into rpt.
func (lr *NoGoroutineRule) Check(aNode ast.Node, fs *token.FileSet, rpt *[]string) {
	if gs, ok := aNode.(*ast.GoStmt); ok {
		report := createLintReport(gs.Pos(), fs, "goroutine is disallowed.")
		*rpt = append(*rpt, report)
	}
}
