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

// NoShortRule requires that testing.Short() is not allowed.
type NoShortRule struct{}

func NewNoShortRule() *NoShortRule {
	return &NoShortRule{}
}

// GetID returns no_short_rule.
func (lr *NoShortRule) GetID() string {
	return getCallerFileName()
}

// Check verifies if aNode is not testing.Short(). If verification fails it adds report into rpt.
func (lr *NoShortRule) Check(aNode ast.Node, fs *token.FileSet, rpt *[]string) {
	if ce, ok := aNode.(*ast.CallExpr); ok {
		if matchCallExpr(ce, "testing", "Short") {

			report := createLintReport(ce.Pos(), fs, "testing.Short() is disallowed.")
			*rpt = append(*rpt, report)
		}
	}
}
