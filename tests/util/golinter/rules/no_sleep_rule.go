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

// NoSleepRule requires that time.Sleep() is not allowed.
type NoSleepRule struct{}

// NewNoSleepRule creates and returns a NoSleepRule object.
func NewNoSleepRule() *NoSleepRule {
	return &NoSleepRule{}
}

// GetID returns no_sleep_rule.
func (lr *NoSleepRule) GetID() string {
	return getCallerFileName()
}

// Check verifies if aNode is not time.Sleep. If verification fails it adds report into rpt.
func (lr *NoSleepRule) Check(aNode ast.Node, fs *token.FileSet, rpt *[]string) {
	if ce, ok := aNode.(*ast.CallExpr); ok {
		if matchCallExpr(ce, "time", "Sleep") {

			report := createLintReport(ce.Pos(), fs, "time.Sleep() is disallowed.")
			*rpt = append(*rpt, report)
		}
	}
}
