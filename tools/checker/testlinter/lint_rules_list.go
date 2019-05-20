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

package main

import (
	"istio.io/istio/tools/checker"
	"istio.io/istio/tools/checker/testlinter/rules"
)

// LintRulesList is a map that maps test type to list of lint rules. Linter applies corresponding
// list of lint rules to each type of tests.
var LintRulesList = map[TestType][]checker.Rule{
	UnitTest: { // list of rules which should apply to unit test file
		rules.NewSkipByIssue(),
	},
	IntegTest: { // list of rules which should apply to integration test file
		rules.NewSkipByIssue(),
	},
	E2eTest: { // list of rules which should apply to e2e test file
		rules.NewSkipByIssue(),
	},
}
