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
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/scopes"
)

// Test allows the test author to specify test-related metadata in a fluent-style, before commencing execution.
type Test struct {
	// name to be used when creating a Golang test. Only used for subtests.
	name        string
	parent      *Test
	goTest      *testing.T
	labels      []label.Instance
	s           *suiteContext
	requiredEnv environment.Name

	ctx             *testContext
	childIsParallel bool
}

// NewTest returns a new test wrapper for running a single test.
func NewTest(t *testing.T) *Test {
	rtMu.Lock()
	defer rtMu.Unlock()

	if rt == nil {
		panic("call to scope without running the test framework")
	}

	runner := &Test{
		s:      rt.suiteContext(),
		goTest: t,
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
	t.requiredEnv = name
	return t
}

// Run the test, supplied as a lambda.
func (t *Test) Run(fn func(ctx TestContext)) {
	// Disallow running the same test more than once.
	if t.ctx != nil {
		testName := t.name
		if testName == "" && t.goTest != nil {
			testName = t.goTest.Name()
		}
		panic(fmt.Sprintf("Attempting to run test `%s` more than once", testName))
	}

	if t.parent != nil {
		// Create a new subtest under the parent's test.
		parentGoTest := t.parent.goTest
		parentCtx := t.parent.ctx
		parentGoTest.Run(t.name, func(goTest *testing.T) {
			t.goTest = goTest
			t.doRun(parentCtx.newChildContext(t), fn)
		})
	} else {
		// Not a child context. Running with the test provided during construction.
		t.doRun(newRootContext(t, t.goTest, t.labels...), fn)
	}
}

// parallel is called by the testContext when the user has selected this test to run in Parallel.
func (t *Test) parallel() {
	if t.parent != nil {
		t.parent.childIsParallel = true
	}
}

func (t *Test) doRun(ctx *testContext, fn func(ctx TestContext)) {
	t.ctx = ctx

	if t.requiredEnv != "" && t.s.Environment().EnvironmentName() != t.requiredEnv {
		ctx.Done()
		t.goTest.Skipf("Skipping %q: expected environment not found: %s", t.goTest.Name(), t.requiredEnv)
		return
	}

	start := time.Now()

	scopes.CI.Infof("=== BEGIN: Test: '%s[%s]' ===", rt.suiteContext().Settings().TestID, t.goTest.Name())
	defer func() {
		doneFn := func() {
			end := time.Now()
			scopes.CI.Infof("=== DONE:  Test: '%s[%s] (%v)' ===",
				rt.suiteContext().Settings().TestID,
				t.goTest.Name(),
				end.Sub(start))
			ctx.Done()
		}
		if t.childIsParallel {
			// If the child is running in parallel, it won't continue until this test returns.
			// Since ctx.Done() will block until the child test is complete, we run ctx.Done()
			// asynchronously.
			go doneFn()
		} else {
			doneFn()
		}
	}()

	fn(ctx)
}
