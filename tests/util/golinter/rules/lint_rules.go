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

// Defines ID for each lint rule. Each rule is assigned a unique ID. AllRules is a special name
// used only in whitelist, which skips all rules for a file path.
const (
	SkipByIssue = "SkipByIssue" // SkipByIssue
	NoSleep     = "NoSleep"     // NoSleep
	NoGoroutine = "NoGoroutine" // NoGoroutine
	SkipByShort = "SkipByShort" // SkipByShort
)

// LintRule is interface for defining lint rules.
type LintRule interface {
	// GetID returns ID of the rule in string.
	GetID() string
	// Check verifies if aNode passes rule check, and add report into linter lt.
	Check(aNode ast.Node, lt *linter.Linter)
}
