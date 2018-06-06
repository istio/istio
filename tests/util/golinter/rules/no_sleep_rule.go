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
	"istio.io/istio/tests/util/golinter/linter"
)

// NoSleepRule requires that time.Sleep() is not allowed.
type NoSleepRule struct{}

func NewNoSleepRule() *NoSleepRule {
	return &NoSleepRule{}
}

// GetID returns NoSleep.
func (lr *NoSleepRule) GetID() string {
	return NoSleep
}

// Check verifies if aNode is not time.Sleep. If verification fails it reports to linter.
func (lr *NoSleepRule) Check(aNode ast.Node, lt *linter.Linter) {
	if ce, ok := aNode.(*ast.CallExpr); ok {
		if matchCallExpr(ce, "time", "Sleep") {

			rpt := createLintReport(ce.Pos(), lt.Fs(), "time.Sleep() is disallowed.")
			lt.LReport() = append(lt.LReport(), rpt)
		}
	}
}
