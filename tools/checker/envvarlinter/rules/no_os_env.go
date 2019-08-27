// Copyright 2019 Istio Authors. All Rights Reserved.
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

// NoOsEnv flags an error if os.Getenv or os.LookupEnv are used.
type NoOsEnv struct {
}

// NewNoOsEnv creates and returns a NoOsEnv object.
func NewNoOsEnv() *NoOsEnv {
	return &NoOsEnv{}
}

// GetID returns skip_by_issue_rule.
func (lr *NoOsEnv) GetID() string {
	return GetCallerFileName()
}

// Check verifies there are no calls to os.Getenv or os.LookupEnv
func (lr *NoOsEnv) Check(aNode ast.Node, fs *token.FileSet, lrp *checker.Report) {
	if ce, ok := aNode.(*ast.CallExpr); ok {
		if MatchCallExpr(ce, "os", "Getenv") {
			lrp.AddItem(fs.Position(ce.Pos()), lr.GetID(), "os.Getenv is disallowed, please see pkg/env instead")
		} else if MatchCallExpr(ce, "os", "LookupEnv") {
			lrp.AddItem(fs.Position(ce.Pos()), lr.GetID(), "os.LookupEnv is disallowed, please see pkg/env instead")
		}
	}
}
