//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package framework

import (
	"os"
	"testing"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework/dependency"
	env "istio.io/istio/pkg/test/framework/environment"
)

var scope = log.RegisterScope("testframework", "General scope for the test framework", 0)
var lab = log.RegisterScope("testframework-lab", "Scope for normal log reporting to be used by the lab", 0)

var d = newDriver()

// Run is a helper for executing test main with appropriate resource allocation/doCleanup steps.
// It allows us to do post-run doCleanup, and flag parsing.
func Run(testID string, m *testing.M) {
	exitcode, err := d.Run(testID, m)
	if err != nil {
		scope.Errorf("test.Run: %v", err)
	}
	os.Exit(exitcode)
}

// SuiteRequires indicates that the whole suite requires particular dependencies.
func SuiteRequires(_ *testing.M, dependencies ...dependency.Instance) {
	if err := d.SuiteRequires(dependencies); err != nil {
		panic(err)
	}
}

// Requires ensures that the given dependencies will be satisfied. If they cannot, then the
// test will fail.
func Requires(t testing.TB, dependencies ...dependency.Instance) {
	t.Helper()
	d.Requires(t, dependencies)
}

// AcquireEnvironment resets and returns the environment. Once AcquireEnvironment should be called exactly
// once per test.
func AcquireEnvironment(t testing.TB) env.Environment {
	t.Helper()
	return d.AcquireEnvironment(t)
}
