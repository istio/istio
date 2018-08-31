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

	"istio.io/istio/tests/util/checker"
	"istio.io/istio/tests/util/checker/parser/rules"
	"log"
)

// RulesMatcher filters out test files and detects test type.
type RulesMatcher struct {
}

// GetRules checks path absp and decides whether absp is a test file. It returns true and rules to
// parse the file. If path absp should be skipped, it returns false.
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
	log.Printf("check path %s", absp)
	// Skip path which is not go test file or is a directory.
	paths := strings.Split(absp, "/")
	if len(paths) == 0 || info.IsDir() || !strings.HasSuffix(absp, "_test.go") {
		return []checker.Rule{}
	}

	return []checker.Rule{rules.NewIsFlaky(),}
}
