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
	"testing"
	"reflect"
	"path/filepath"
	"istio.io/istio/tests/util/golinter/linter"
	"istio.io/istio/tests/util/golinter/rules"
)

func getAbsPath(path string) string {
	if !filepath.IsAbs(path) {
		path, _ = filepath.Abs(path)
	}
	return path
}

func clearLintRulesList() {
	delete(linter.LintRulesList, linter.UnitTest)
	delete(linter.LintRulesList, linter.IntegTest)
	delete(linter.LintRulesList, linter.E2eTest)
}

func TestUnitTestSkipByIssueRule(t *testing.T) {
	clearLintRulesList()
	linter.LintRulesList[linter.UnitTest] = []rules.LintRule{rules.NewSkipByIssueRule(),}

	rpts := getReport([]string{"testdata/"})
	expectedRpts := []string{getAbsPath("testdata/unit_test.go") + ":10:2:Only t.Skip() is allowed and t.Skip() should contain an url to GitHub issue."}

	if !reflect.DeepEqual(rpts, expectedRpts) {
		t.Errorf("lint reports don't match\nReceived: %v\nExpected: %v", rpts, expectedRpts)
	}
}

func TestUnitTestNoShortRule(t *testing.T) {
	clearLintRulesList()
	linter.LintRulesList[linter.UnitTest] = []rules.LintRule{rules.NewNoShortRule(),}

	rpts := getReport([]string{"testdata/"})
	expectedRpts := []string{getAbsPath("testdata/unit_test.go") + ":33:5:testing.Short() is disallowed."}

	if !reflect.DeepEqual(rpts, expectedRpts) {
		t.Errorf("lint reports don't match\nReceived: %v\nExpected: %v", rpts, expectedRpts)
	}
}

func TestUnitTestNoSleepRule(t *testing.T) {
	clearLintRulesList()
	linter.LintRulesList[linter.UnitTest] = []rules.LintRule{rules.NewNoSleepRule(),}

	rpts := getReport([]string{"testdata/"})
	expectedRpts := []string{getAbsPath("testdata/unit_test.go") + ":50:2:time.Sleep() is disallowed."}

	if !reflect.DeepEqual(rpts, expectedRpts) {
		t.Errorf("lint reports don't match\nReceived: %v\nExpected: %v", rpts, expectedRpts)
	}
}

func TestUnitTestNoGoroutineRule(t *testing.T) {
	clearLintRulesList()
	linter.LintRulesList[linter.UnitTest] = []rules.LintRule{rules.NewNoGoroutineRule(),}

	rpts := getReport([]string{"testdata/"})
	expectedRpts := []string{getAbsPath("testdata/unit_test.go") + ":58:2:goroutine is disallowed."}

	if !reflect.DeepEqual(rpts, expectedRpts) {
		t.Errorf("lint reports don't match\nReceived: %v\nExpected: %v", rpts, expectedRpts)
	}
}

func TestUnitTestWhitelist(t *testing.T) {
	clearLintRulesList()
	linter.LintRulesList[linter.UnitTest] = []rules.LintRule{rules.NewSkipByIssueRule(),
	rules.NewNoShortRule(),
	rules.NewNoSleepRule(),
	rules.NewNoGoroutineRule(),}
	linter.WhitelistPath = make(map[string][]string)
	linter.WhitelistPath["testdata/unit_test.go"] = []string{"skip_by_issue_rule", "no_short_rule",
	"no_sleep_rule", "no_goroutine_rule",}

	rpts := getReport([]string{"testdata/*"})
	expectedRpts := []string{}

	if !reflect.DeepEqual(rpts, expectedRpts) {
		t.Errorf("lint reports don't match\nReceived: %v\nExpected: %v", rpts, expectedRpts)
	}
}