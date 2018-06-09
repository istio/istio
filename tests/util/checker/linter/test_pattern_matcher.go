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

// PathFilter filters out test files and detects test type.
type PathFilter struct {
	WPaths map[string]map[string]bool // absolute paths that are whitelisted.
}

// NewPathFilter creates a new PathFilter object.
func NewPathFilter() PathFilter {
	p := PathFilter{map[string]map[string]bool{}}
	p.getWhitelistedPathsMap()
	return p
}

// getWhitelistedPathsMap converts whitelistedPaths to a map that maps path to rules
func (pf *PathFilter) getWhitelistedPathsMap() {
	for path, rules := range WhitelistPath {
		pf.WPaths[path] = map[string]bool{}
		for _, rule := range rules {
			pf.WPaths[path][rule] = true
		}
	}
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
func (pf *PathFilter) GetRules(absp string, info os.FileInfo) []checker.Rule {
	// sRules stores skipped rules for file path absp.
	var sRules = map[string]bool{}

	paths := strings.Split(absp, "/")
	if len(paths) == 0 {
		return []checker.Rule{}
	}

	// Skip path which is not go file.
	if info.IsDir() || !strings.HasSuffix(absp, ".go") {
		return []checker.Rule{}
	}

	// Check whether path is whitelisted
	for wp, ruleMap := range pf.WPaths {
		matched, err := filepath.Match(wp, absp)
		if err != nil {
			log.Printf("file match returns error: %v", err)
		}
		if matched {
			sRules = ruleMap
		}
	}

	var isUnderE2eDir, isUnderIntegDir = false, false
	for _, path := range paths {
		if path == "e2e" {
			isUnderE2eDir = true
		} else if path == "integ" {
			isUnderIntegDir = true
		}
	}

	testType := NonTest
	if isUnderE2eDir && isUnderIntegDir {
		// TODO(Jimmy) this should probably be a lint error instesd...
	} else if isUnderE2eDir && strings.HasSuffix(paths[len(paths)-1], "_test.go") {
		testType = E2eTest
	} else if (isUnderIntegDir && strings.HasSuffix(paths[len(paths)-1], "_test.go")) ||
		strings.HasSuffix(paths[len(paths)-1], "_integ_test.go") {
		testType = IntegTest
	} else if strings.HasSuffix(paths[len(paths)-1], "_test.go") &&
		!strings.HasSuffix(paths[len(paths)-1], "_integ_test.go") {
		testType = UnitTest
	}
	rules := []checker.Rule{}
	for _, rule := range LintRulesList[testType] {
		if _, skip := sRules[rule.GetID()]; !skip {
			rules = append(rules, rule)
		}
	}
	return rules
}
