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
	"log"
	"os"
	"path/filepath"
	"strings"
)

// whitelistedPaths contains paths of files and directories that skip linting.
// Paths could be relevant paths or absolute paths.
// For example,
// ../../../mixer/test/client/check_cache/check_cache_test.go
// ../../../mixer/test/client/check_cache_hit/
var whitelistedPaths = []string{}

// forbiddenFunctionCall lists all the forbidden functions in <package name>.<method name> format.
var forbiddenFunctionCalls = []string{
	"time.Sleep",
	"testing.Short",
}

// TestType is type ID of tests
type TestType int

// All types of tests to parse.
const (
	UnitTest  TestType = iota // UnitTest == 0
	IntegTest TestType = iota // IntegTest == 1
	E2eTest   TestType = iota // E2eTest == 2
	NonTest   TestType = iota // NonTest == 3
)

// TestTypeToString contains test types in string. The order should be in line with enum above.
var TestTypeToString = []string{"unit test", "integration test", "e2e test", "None"}

type pathFilter struct {
	absWPaths map[string]bool // absolute paths that are whitelisted.
}

func newPathFilter() pathFilter {
	p := pathFilter{make(map[string]bool)}
	p.getAbsWhitelistedPaths()
	return p
}

// getAbsWhitelistedPaths converts paths from whitelistedPaths to absolute paths.
func (pf *pathFilter) getAbsWhitelistedPaths() {
	for _, path := range whitelistedPaths {
		if !filepath.IsAbs(path) {
			path, _ = filepath.Abs(path)
		}
		pf.absWPaths[path] = true
	}
}

// IsTestFile checks path absp and desides whether absp is a test file. It returns true and test type
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
func (pf *pathFilter) IsTestFile(absp string, info os.FileInfo) (bool, TestType) {
	// Skip path that is whitelisted.
	if _, ok := pf.absWPaths[absp]; ok {
		return false, NonTest
	}

	// Skip path which is not go file.
	if info.IsDir() || !strings.HasSuffix(absp, ".go") {
		return false, NonTest
	}

	paths := strings.Split(absp, "/")
	if len(paths) == 0 {
		return false, NonTest
	}

	var isUnderE2eDir, isUnderIntegDir = false, false
	for _, path := range paths {
		if path == "e2e" {
			isUnderE2eDir = true
		} else if path == "integ" {
			isUnderIntegDir = true
		}
	}

	if isUnderE2eDir && isUnderIntegDir {
		log.Printf("Invalid path %q under both e2e directory and integ directory", absp)
		return false, NonTest
	} else if isUnderE2eDir && strings.HasSuffix(paths[len(paths)-1], "_test.go") {
		return true, E2eTest
	} else if (isUnderIntegDir && strings.HasSuffix(paths[len(paths)-1], "_test.go")) ||
		strings.HasSuffix(paths[len(paths)-1], "_integ_test.go") {
		return true, IntegTest
	} else if strings.HasSuffix(paths[len(paths)-1], "_test.go") &&
		!strings.HasSuffix(paths[len(paths)-1], "_integ_test.go") {
		return true, UnitTest
	}
	return false, NonTest
}
