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
	"os"
	"strings"

	"istio.io/istio/tools/checker"
)

// TestType is type ID of tests
type TestType int

// All types of tests to parse.
const (
	UnitTest  TestType = iota // UnitTest == 0
	IntegTest TestType = iota // IntegTest == 1
	E2eTest   TestType = iota // E2eTest == 2
)

// RulesMatcher filters out test files and detects test type.
type RulesMatcher struct {
}

// GetRules checks path absp and decides whether absp is a test file. It returns true and test type
// for a test file. If path absp should be skipped, it returns false.
// If one of the following cases meet, path absp is a valid path to test file.
// (1) e2e test file
// .../e2e/.../*_test.go
// (2) integration test file
// .../integration/.../*_test.go
// .../integration/.../*_integ_test.go
// .../*_integ_test.go
// (3) unit test file
// .../*_test.go
func (rf *RulesMatcher) GetRules(absp string, info os.FileInfo) []checker.Rule {
	// Skip path which is not go test file or is a directory.
	paths := strings.Split(absp, "/")
	if len(paths) == 0 || info.IsDir() || !strings.HasSuffix(absp, "_test.go") {
		return []checker.Rule{}
	}

	for _, path := range paths {
		if path == "e2e" {
			return LintRulesList[E2eTest]
		} else if path == "integration" {
			return LintRulesList[IntegTest]
		}
	}
	if strings.HasSuffix(paths[len(paths)-1], "_integ_test.go") {
		// Integration tests can be in non integration directories.
		return LintRulesList[IntegTest]
	}
	return LintRulesList[UnitTest]
}
