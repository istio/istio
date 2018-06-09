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

package linter

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"istio.io/istio/tests/util/checker"
)

// TestType is type ID of tests
type TestType int

// All types of tests to parse.
const (
	UnitTest  TestType = iota // UnitTest == 0
	IntegTest TestType = iota // IntegTest == 1
	E2eTest   TestType = iota // E2eTest == 2
	NonTest   TestType = iota // NonTest == 3
)

// RulesMatcher filters out test files and detects test type.
type RulesMatcher struct {
}

// GetTestType checks path absp and desides whether absp is a test file. It returns true and test type
// for a test file. If path absp should be skipped, it returns false.
// If one of the following cases meet, path absp is a valid path to test file.
// (1) e2e test file
// .../e2e/.../*_test.go
// (2) integration test file
// .../integ/.../*_test.go
// .../integ/.../*_integ_test.go
// .../*_integ_test.go
// (3) unit test file
// .../*_test.go
func (rf *RulesMatcher) GetRules(absp string, info os.FileInfo) []checker.Rule {
	// Skip path which is not go test file or is a directory.
	paths := strings.Split(absp, "/")
	if len(paths) == 0 || info.IsDir() || !strings.HasSuffix(absp, "_test.go") {
		return []checker.Rule{}
	}

	testType := NonTest
	for _, path := range paths {
		if path == "e2e" {
			testType = E2eTest
		} else if path == "integ" {
			testType = IntegTest
		}
	}
	if strings.HasSuffix(paths[len(paths)-1], "_integ_test.go") {
		// Integration tests can be in non integ directories.
		testType = IntegTest
	} else if testType == NonTest {
		testType = UnitTest
	}

	skipRules := getWhitelistedRules(absp)
	rules := []checker.Rule{}
	for _, rule := range LintRulesList[testType] {
		if !shouldSkip(rule, skipRules) {
			rules = append(rules, rule)
		}
	}
	return rules
}

// getWhitelistedRules returns the whitelisted rule given the path
func getWhitelistedRules(absp string) []string {
	// skipRules stores skipped rules for file path absp.
	skipRules := []string{}

	// Check whether path is whitelisted
	for wp, whitelistedRules := range PathWhitelist {
		// filepath.Match is needed for canonical matching
		matched, err := filepath.Match(wp, absp)
		if err != nil {
			log.Printf("file match returns error: %v", err)
		}
		if matched {
			skipRules = whitelistedRules
		}
	}
	return skipRules
}

// shouldSkip checks if the given rule should be skipped
func shouldSkip(rule checker.Rule, skipeRules []string) bool {
	shouldSkip := false
	for _, skipRule := range skipeRules {
		if skipRule == rule.GetID() {
			shouldSkip = true
		}
	}
	return shouldSkip
}