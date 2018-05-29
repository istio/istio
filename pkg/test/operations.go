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

package test

import (
	"os"
	"testing"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/dependency"
	"istio.io/istio/pkg/test/environment"
	"istio.io/istio/pkg/test/impl/driver"
	"istio.io/istio/pkg/test/label"
)

var scope = log.RegisterScope("testframework", "General scope for the test framework", 0)

var d = driver.New()

// Run is a helper for executing test main with appropriate resource allocation/doCleanup steps.
// It allows us to do post-run doCleanup, and flag parsing.
func Run(testID string, m *testing.M) {
	if err := processFlags(); err != nil {
		scope.Errorf("test.Run: log options error: '%v'", err)
		os.Exit(-1)
	}

	args := *arguments
	args.TestID = testID
	args.M = m

	scope.Debugf("test.Run: command-line flags are parsed, and logging is initialized.")
	scope.Debugf("test.Run: log options: %+v", logOptions)
	scope.Debugf("test.Run: driver args: %+v", args)

	if err := d.Initialize(&args); err != nil {
		scope.Errorf("test.Run: initialization error: '%v'", err)
		os.Exit(-1)
	}

	scope.Infof(">>> Beginning test run for: '%s'", testID)
	rt := d.Run()
	scope.Infof("<<< Completing test run for: '%s'", testID)

	os.Exit(rt)
}

// Ignore the test with the given reason.
func Ignore(t testing.TB, reason string) {
	t.Helper()
	t.Skipf("Skipping(Ignored): %s", reason)
}

// SuiteRequires indicates that the whole suite requires particular dependencies.
func SuiteRequires(m *testing.M, dependencies ...dependency.Instance) {
	// We only care that m exists at this point.
	if m == nil {
		panic("test.SuiteRequires: nil testing.M")
	}

	arguments.SuiteDependencies = append(arguments.SuiteDependencies, dependencies...)
}

// Requires ensures that the given dependencies will be satisfied. If they cannot, then the
// test will fail.
func Requires(t testing.TB, dependencies ...dependency.Instance) {
	t.Helper()
	d.InitializeTestDependencies(t, dependencies)
}

// SuiteTag tags all tests in the suite with the given labels.
func SuiteTag(m *testing.M, labels ...label.Label) {
	// We only care that m exists at this point.
	if m == nil {
		panic("test.SuiteTag: nil testing.M")
	}

	arguments.SuiteLabels = append(arguments.SuiteLabels, labels...)
}

// Tag the test with the given labels. The user can filter using the labels.
// TODO: The polarity of this is a bit borked. If the test doesn't call Tag, then it won't get filtered out.
func Tag(t testing.TB, labels ...label.Label) {
	t.Helper()
	d.CheckLabels(t, labels)
}

// AcquireEnvironment resets and returns the environment. Once AcquireEnvironment should be called exactly
// once per test.
func AcquireEnvironment(t testing.TB) environment.Interface {
	t.Helper()
	return d.AcquireEnvironment(t)
}
