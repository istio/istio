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

// NoSleep requires that time.Sleep() is not allowed.
type NoSleep struct{}

// NewNoSleep creates and returns a NoSleep object.
func NewNoSleep() *NoSleep {
	return &NoSleep{}
}

// GetID returns no_sleep_rule.
func (lr *NoSleep) GetID() string {
	return GetCallerFileName()
}

// Check verifies if aNode is not time.Sleep. If verification fails lrp creates a new report.
func (lr *NoSleep) Check(aNode ast.Node, fs *token.FileSet, lrp *checker.Report) {
	if ce, ok := aNode.(*ast.CallExpr); ok {
		if MatchCallExpr(ce, "time", "Sleep") {
			lrp.AddItem(fs.Position(ce.Pos()), lr.GetID(), "time.Sleep() is disallowed.")
		}
	}
}
