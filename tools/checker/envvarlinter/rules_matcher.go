// Copyright 2019 Istio Authors. All Rights Reserved.
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

	"istio.io/istio/tools/checker/envvarlinter/rules"

	"istio.io/istio/tools/checker"
)

// RulesMatcher filters out test files.
type RulesMatcher struct {
}

// GetRules checks path absp and decides whether absp is a test file. It returns true and test type
// for a test file. If path absp should be skipped, it returns false.
func (rf *RulesMatcher) GetRules(absp string, info os.FileInfo) []checker.Rule {
	// Skip path which is a directory, a go test file, or not a go file at all.
	paths := strings.Split(absp, "/")
	if len(paths) == 0 || info.IsDir() || strings.HasSuffix(absp, "_test.go") || !strings.HasSuffix(absp, ".go") {
		return []checker.Rule{}
	}

	return []checker.Rule{rules.NewNoOsEnv()}
}
