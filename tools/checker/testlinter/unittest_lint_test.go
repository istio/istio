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
	"path/filepath"
	"reflect"
	"testing"

	"istio.io/istio/tools/checker"
	"istio.io/istio/tools/checker/testlinter/rules"
)

func getAbsPath(path string) string {
	if !filepath.IsAbs(path) {
		path, _ = filepath.Abs(path)
	}
	return path
}

func clearLintRulesList() {
	checker.IgnoreTestLinterData = false
	delete(LintRulesList, UnitTest)
	delete(LintRulesList, IntegTest)
	delete(LintRulesList, E2eTest)
}

func TestUnitTestSkipByIssueRule(t *testing.T) {
	clearLintRulesList()
	LintRulesList[UnitTest] = []checker.Rule{rules.NewSkipByIssue()}

	rpts, _ := getReport([]string{"testdata/"})
	expectedRpts := []string{getAbsPath("testdata/unit_test.go") +
		":24:2:Only t.Skip() is allowed and t.Skip() should contain an url to GitHub issue. (skip_issue)"}

	if !reflect.DeepEqual(rpts, expectedRpts) {
		t.Errorf("lint reports don't match\nReceived: %v\nExpected: %v", rpts, expectedRpts)
	}
}

func TestUnitTestNoShortRule(t *testing.T) {
	clearLintRulesList()
	LintRulesList[UnitTest] = []checker.Rule{rules.NewNoShort()}

	rpts, _ := getReport([]string{"testdata/"})
	expectedRpts := []string{getAbsPath("testdata/unit_test.go") + ":48:5:testing.Short() is disallowed. (no_short)"}

	if !reflect.DeepEqual(rpts, expectedRpts) {
		t.Errorf("lint reports don't match\nReceived: %v\nExpected: %v", rpts, expectedRpts)
	}
}

func TestUnitTestNoSleepRule(t *testing.T) {
	clearLintRulesList()
	LintRulesList[UnitTest] = []checker.Rule{rules.NewNoSleep()}

	rpts, _ := getReport([]string{"testdata/"})
	expectedRpts := []string{getAbsPath("testdata/unit_test.go") + ":66:2:time.Sleep() is disallowed. (no_sleep)"}

	if !reflect.DeepEqual(rpts, expectedRpts) {
		t.Errorf("lint reports don't match\nReceived: %v\nExpected: %v", rpts, expectedRpts)
	}
}

func TestUnitTestNoGoroutineRule(t *testing.T) {
	clearLintRulesList()
	LintRulesList[UnitTest] = []checker.Rule{rules.NewNoGoroutine()}

	rpts, _ := getReport([]string{"testdata/"})
	expectedRpts := []string{getAbsPath("testdata/unit_test.go") + ":75:2:goroutine is disallowed. (no_goroutine)"}

	if !reflect.DeepEqual(rpts, expectedRpts) {
		t.Errorf("lint reports don't match\nReceived: %v\nExpected: %v", rpts, expectedRpts)
	}
}

func TestUnitTestWhitelist(t *testing.T) {
	clearLintRulesList()
	LintRulesList[UnitTest] = []checker.Rule{rules.NewSkipByIssue(),
		rules.NewNoShort(),
		rules.NewNoSleep(),
		rules.NewNoGoroutine()}
	Whitelist = make(map[string][]string)
	Whitelist["testdata/unit_test.go"] = []string{"skip_by_issue_rule", "no_short_rule",
		"no_sleep_rule", "no_goroutine_rule"}

	rpts, _ := getReport([]string{"testdata/*"})
	expectedRpts := []string{}

	if (len(rpts) > 0 || len(expectedRpts) > 0) && !reflect.DeepEqual(rpts, expectedRpts) {
		t.Errorf("lint reports don't match\nReceived: %v\nExpected: %v", rpts, expectedRpts)
	}
}
