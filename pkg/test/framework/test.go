// Copyright 2019 Istio Authors
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

package framework

import (
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/scopes"
)

// Test allows the test author to specify test-related metadata in a fluent-style, before commencing execution.
type Test struct {
	t      *testing.T
	labels []label.Instance
	s      SuiteContext
}

// NewTest returns a new test wrapper for running a single test.
func NewTest(t *testing.T) *Test {
	rtMu.Lock()
	defer rtMu.Unlock()

	if rt == nil {
		panic("call to scope without running the test framework")
	}

	runner := &Test{
		s: rt.SuiteContext(),
		t: t,
	}

	return runner
}

// Label applies the given labels to this test.
func (t *Test) Label(labels ...label.Instance) *Test {
	t.labels = append(t.labels, labels...)
	return t
}

// RequiresEnvironment ensures that the current environment matches what the suite expects. Otherwise it stops test
// execution and skips the test.
func (t *Test) RequiresEnvironment(name environment.Name) *Test {
	if t.s.Environment().EnvironmentName() != name {
		t.t.Skipf("Skipping %q: expected environment not found: %s", t.t.Name(), name)
	}

	return t
}

// Run the test, supplied as a lambda.
func (t *Test) Run(fn func(ctx TestContext)) {
	start := time.Now()

	scopes.CI.Infof("=== BEGIN: Test: '%s[%s]' ===", rt.SuiteContext().Settings().TestID, t.t.Name())
	defer func() {
		end := time.Now()
		scopes.CI.Infof("=== DONE:  Test: '%s[%s] (%v)' ===", rt.SuiteContext().Settings().TestID, t.t.Name(), end.Sub(start))
	}()

	ctx := NewContext(t.t, t.labels...)
	defer ctx.Done(t.t)
	fn(ctx)
}
