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
	"fmt"
	"go/ast"
	"go/token"
	"istio.io/istio/tests/util/golinter/linter"
)

// NoGoroutineRule requires that go f(x, y, z) is not allowed.
type NoGoroutineRule struct{}

func NewNoGoroutineRule() *NoGoroutineRule {
	return &NoGoroutineRule{}
}

// GetID returns NoGoroutine.
func (lr *NoGoroutineRule) GetID() string {
	return NoGoroutine
}

// Check verifies if aNode is not goroutine. If verification fails it reports to linter.
func (lr *NoGoroutineRule) Check(aNode ast.Node, lt *linter.Linter) {
	if gs, ok := aNode.(*ast.GoStmt); ok {
		rpt := createLintReport(gs.Pos(), lt.Fs(), "goroutine is disallowed.")
		lt.LReport() = append(lt.LReport(), rpt)
	}
}
