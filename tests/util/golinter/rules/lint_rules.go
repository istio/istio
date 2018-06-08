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

// LintRule is interface for defining lint rules.
type LintRule interface {
	// GetID returns ID of the rule in string, ID is equal to the file name of that rule.
	GetID() string
	// Check verifies if aNode passes rule check. If verification fails lrp creates a report.
	Check(aNode ast.Node, fs *token.FileSet, lrp *LintReporter)
}

// LintReporter populates lint report.
type LintReporter struct {
	reports *[]string
}

// NewLintReport creates and returns a LintReporter object.
func NewLintReport(rpts *[]string) *LintReporter {
	return &LintReporter{
		reports: rpts,
	}
}

// AddReport creates a new lint error report.
func (lr *LintReporter) AddReport(pos token.Pos, fs *token.FileSet, msg string) {
	report := createLintReport(pos, fs, msg)
	*lr.reports = append(*lr.reports, report)
}
