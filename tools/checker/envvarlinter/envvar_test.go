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
	"path/filepath"
	"reflect"
	"testing"
)

func getAbsPath(path string) string {
	if !filepath.IsAbs(path) {
		path, _ = filepath.Abs(path)
	}
	return path
}

func TestNoOSEnvRule(t *testing.T) {
	rpts, _ := getReport([]string{"testdata/"})
	expectedRpts := []string{getAbsPath("testdata/envuse.go") +
		":20:6:os.Getenv is disallowed, please see pkg/env instead (no_os_env)",
		getAbsPath("testdata/envuse.go") +
			":21:9:os.LookupEnv is disallowed, please see pkg/env instead (no_os_env)"}

	if !reflect.DeepEqual(rpts, expectedRpts) {
		t.Errorf("lint reports don't match\nReceived: %v\nExpected: %v", rpts, expectedRpts)
	}
}
