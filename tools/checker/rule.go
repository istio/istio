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

package checker

import (
	"go/ast"
	"go/token"
	"os"
)

// Rule is interface for defining lint rules.
type Rule interface {
	// GetID returns ID of the rule in string, ID is equal to the file name of that rule.
	GetID() string
	// Check verifies if aNode passes rule check. If verification fails lrp creates a report.
	Check(aNode ast.Node, fs *token.FileSet, lrp *Report)
}

// RulesFactory is interface to get Rules from a file path.
type RulesFactory interface {
	// GetRules returns a list of rules used to check against the files.
	GetRules(absp string, info os.FileInfo) []Rule
}
