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

	"istio.io/istio/tools/checker"
)

// NoGoroutine requires that go f(x, y, z) is not allowed.
type NoGoroutine struct{}

// NewNoGoroutine creates and returns a NoGoroutine object.
func NewNoGoroutine() *NoGoroutine {
	return &NoGoroutine{}
}

// GetID returns no_goroutine_rule.
func (lr *NoGoroutine) GetID() string {
	return GetCallerFileName()
}

// Check verifies if aNode is not goroutine. If verification fails lrp creates new report.
func (lr *NoGoroutine) Check(aNode ast.Node, fs *token.FileSet, lrp *checker.Report) {
	if gs, ok := aNode.(*ast.GoStmt); ok {
		lrp.AddItem(fs.Position(gs.Pos()), lr.GetID(), "goroutine is disallowed.")
	}
}
