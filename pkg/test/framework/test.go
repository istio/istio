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

	ctx *testContext

	// Indicates that at least one child test is being run in parallel. In Go, when
	// t.Parallel() is called on a test, execution is halted until the parent test exits.
	// Only after that point, are the Parallel children are resumed. Because the parent test
	// must exit before the Parallel children do, we have to defer closing the parent's
	// testcontext until after the children have completed.
	hasParallelChildren bool
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
	t.runInternal(fn, false)
}

// RunParallel runs this test in parallel with other children of the same parent test/suite. Under the hood,
// this relies on Go's t.Parallel() and will, therefore, have the same behavior.
//
// A parallel test will run in parallel with siblings that share the same parent test. The parent test function
// will exit before the parallel children are executed. It should be noted that if the parent test is prevented
// from exiting (e.g. parent test is waiting for something to occur within the child test), the test will
// deadlock.
//
// Example:
//
// func TestParallel(t *testing.T) {
//     framework.NewTest(t).
//         Run(func(ctx framework.TestContext) {
//             ctx.NewSubTest("T1").
//                 Run(func(ctx framework.TestContext) {
//                     ctx.NewSubTest("T1a").
//                         RunParallel(func(ctx framework.TestContext) {
//                             // Run in parallel with T1b
//                         })
//                     ctx.NewSubTest("T1b").
//                         RunParallel(func(ctx framework.TestContext) {
//                             // Run in parallel with T1a
//                         })
//                     // Exits before T1a and T1b are run.
//                 })
//
//             ctx.NewSubTest("T2").
//                 Run(func(ctx framework.TestContext) {
//                     ctx.NewSubTest("T2a").
//                         RunParallel(func(ctx framework.TestContext) {
//                             // Run in parallel with T2b
//                         })
//                     ctx.NewSubTest("T2b").
//                         RunParallel(func(ctx framework.TestContext) {
//                             // Run in parallel with T2a
//                         })
//                     // Exits before T2a and T2b are run.
//                 })
//         })
// }
//
// In the example above, non-parallel parents T1 and T2 contain parallel children T1a, T1b, T2a, T2b.
//
// Since both T1 and T2 are non-parallel, they are run synchronously: T1 followed by T2. After T1 exits,
// T1a and T1b are run asynchronously with each other. After T1a and T1b complete, T2 is then run in the
// same way: T2 exits, then T2a and T2b are run asynchronously to completion.
func (t *Test) RunParallel(fn func(ctx TestContext)) {
	t.runInternal(fn, true)
}

func (t *Test) runInternal(fn func(ctx TestContext), parallel bool) {
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
			t.doRun(parentCtx.newChildContext(t), fn, parallel)
		})
	} else {
		// Not a child context. Running with the test provided during construction.
		t.doRun(newRootContext(t, t.goTest, t.labels...), fn, parallel)
	}
}

func (t *Test) doRun(ctx *testContext, fn func(ctx TestContext), parallel bool) {
	// Initial setup if we're running in Parallel.
	if parallel {
		// Inform the parent, who will need to call ctx.Done asynchronously.
		if t.parent != nil {
			t.parent.hasParallelChildren = true
		}

		// Run the underlying Go test in parallel. This will not return until the parent
		// test (if there is one) exits.
		t.goTest.Parallel()
	}

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
		if t.hasParallelChildren {
			// If a child is running in parallel, it won't continue until this test returns.
			// Since ctx.Done() will block until the child test is complete, we run ctx.Done()
			// asynchronously.
			go doneFn()
		} else {
			doneFn()
		}
	}()

	fn(ctx)
}
