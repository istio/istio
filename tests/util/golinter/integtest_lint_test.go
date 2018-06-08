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
	"reflect"
	"testing"

	"istio.io/istio/tests/util/golinter/linter"
	"istio.io/istio/tests/util/golinter/rules"
)

func TestIntegTestSkipByIssueRule(t *testing.T) {
	clearLintRulesList()
	linter.LintRulesList[linter.IntegTest] = []rules.LintRule{rules.NewSkipByIssueRule()}

	rpts := getReport([]string{"testdata/"})
	expectedRpts := []string{
		getAbsPath("testdata/integ/integtest_test.go") + ":9:2:Only t.Skip() is allowed and t.Skip() should contain an url to GitHub issue.",
		getAbsPath("testdata/integtest_integ_test.go") + ":9:2:Only t.Skip() is allowed and t.Skip() should contain an url to GitHub issue."}

	if !reflect.DeepEqual(rpts, expectedRpts) {
		t.Errorf("lint reports don't match\nReceived: %v\nExpected: %v", rpts, expectedRpts)
	}
}

func TestIntegTestSkipByShortRule(t *testing.T) {
	clearLintRulesList()
	linter.LintRulesList[linter.IntegTest] = []rules.LintRule{rules.NewSkipByShortRule()}

	rpts := getReport([]string{"testdata/"})
	expectedRpts := []string{getAbsPath("testdata/integ/integtest_test.go") +
		":8:1:Missing either 'if testing.Short() { t.Skip() }' or 'if !testing.Short() {}'",
		getAbsPath("testdata/integ/integtest_test.go") +
			":23:1:Missing either 'if testing.Short() { t.Skip() }' or 'if !testing.Short() {}'",
		getAbsPath("testdata/integtest_integ_test.go") +
			":8:1:Missing either 'if testing.Short() { t.Skip() }' or 'if !testing.Short() {}'",
		getAbsPath("testdata/integtest_integ_test.go") +
			":23:1:Missing either 'if testing.Short() { t.Skip() }' or 'if !testing.Short() {}'"}

	if !reflect.DeepEqual(rpts, expectedRpts) {
		t.Errorf("lint reports don't match\nReceived: %v\nExpected: %v", rpts, expectedRpts)
	}
}
