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

	"istio.io/istio/tools/checker"
	"istio.io/istio/tools/checker/testlinter/rules"
)

func TestIntegTestSkipByIssueRule(t *testing.T) {
	clearLintRulesList()
	LintRulesList[IntegTest] = []checker.Rule{rules.NewSkipByIssue()}

	rpts, _ := getReport([]string{"testdata/"})
	expectedRpts := []string{
		getAbsPath("testdata/integration/integtest_test.go") + ":12:2:Only t.Skip() is allowed and t.Skip() should contain an url to GitHub issue. (skip_issue)",
		getAbsPath("testdata/integtest_integ_test.go") + ":12:2:Only t.Skip() is allowed and t.Skip() should contain an url to GitHub issue. (skip_issue)"}

	if !reflect.DeepEqual(rpts, expectedRpts) {
		t.Errorf("lint reports don't match\nReceived: %v\nExpected: %v", rpts, expectedRpts)
	}
}

func TestIntegTestSkipByShortRule(t *testing.T) {
	clearLintRulesList()
	LintRulesList[IntegTest] = []checker.Rule{rules.NewSkipByShort()}

	rpts, _ := getReport([]string{"testdata/"})
	expectedRpts := []string{getAbsPath("testdata/integration/integtest_test.go") +
		":11:1:Missing either 'if testing.Short() { t.Skip() }' or 'if !testing.Short() {}' (short_skip)",
		getAbsPath("testdata/integration/integtest_test.go") +
			":27:1:Missing either 'if testing.Short() { t.Skip() }' or 'if !testing.Short() {}' (short_skip)",
		getAbsPath("testdata/integtest_integ_test.go") +
			":11:1:Missing either 'if testing.Short() { t.Skip() }' or 'if !testing.Short() {}' (short_skip)",
		getAbsPath("testdata/integtest_integ_test.go") +
			":27:1:Missing either 'if testing.Short() { t.Skip() }' or 'if !testing.Short() {}' (short_skip)"}

	if !reflect.DeepEqual(rpts, expectedRpts) {
		t.Errorf("lint reports don't match\nReceived: %v\nExpected: %v", rpts, expectedRpts)
	}
}
