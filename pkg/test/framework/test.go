// Copyright Istio Authors
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
	"strings"
	"sync"
	"testing"
	"time"

	"istio.io/pkg/log"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/scopes"
)

type Test interface {
	// Label applies the given labels to this test.
	Label(labels ...label.Instance) Test
	// Label applies the given labels to this test.
	Features(feats ...features.Feature) Test
	NotImplementedYet(features ...features.Feature) Test
	// RequiresMinClusters ensures that the current environment contains at least the expected number of clusters.
	// Otherwise it stops test execution and skips the test.
	RequiresMinClusters(minClusters int) Test
	// RequiresMaxClusters ensures that the current environment contains at most the expected number of clusters.
	// Otherwise it stops test execution and skips the test.
	RequiresMaxClusters(maxClusters int) Test
	// RequiresSingleCluster this a utility that requires the min/max clusters to both = 1.
	RequiresSingleCluster() Test
	// Run the test, supplied as a lambda.
	Run(fn func(ctx TestContext))
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
	RunParallel(fn func(ctx TestContext))
}

// Test allows the test author to specify test-related metadata in a fluent-style, before commencing execution.
type testImpl struct {
	// name to be used when creating a Golang test. Only used for subtests.
	name   string
	parent *testImpl
	goTest *testing.T
	labels []label.Instance
	// featureLabels maps features to the scenarios they cover.
	featureLabels       map[features.Feature][]string
	notImplemented      bool
	s                   *suiteContext
	requiredMinClusters int
	requiredMaxClusters int

	ctx *testContext

	// Indicates that at least one child test is being run in parallel. In Go, when
	// t.Parallel() is called on a test, execution is halted until the parent test exits.
	// Only after that point, are the Parallel children are resumed. Because the parent test
	// must exit before the Parallel children do, we have to defer closing the parent's
	// testcontext until after the children have completed.
	hasParallelChildren bool
}

// globalCleanupLock defines a global wait group to synchronize cleanup of test suites
var globalParentLock = new(sync.Map)

// NewTest returns a new test wrapper for running a single test.
func NewTest(t *testing.T) Test {
	if analyze() {
		return newTestAnalyzer(t)
	}
	rtMu.Lock()
	defer rtMu.Unlock()

	if rt == nil {
		panic("call to scope without running the test framework")
	}

	runner := &testImpl{
		s:             rt.suiteContext(),
		goTest:        t,
		featureLabels: make(map[features.Feature][]string),
	}

	return runner
}

func (t *testImpl) Label(labels ...label.Instance) Test {
	t.labels = append(t.labels, labels...)
	return t
}

func (t *testImpl) Features(feats ...features.Feature) Test {
	c, err := features.BuildChecker(env.IstioSrc + "/pkg/test/framework/features/features.yaml")
	if err != nil {
		log.Errorf("Unable to build feature checker: %s", err)
		t.goTest.FailNow()
		return nil
	}
	for _, f := range feats {
		check, scenario := c.Check(f)
		if !check {
			log.Errorf("feature %s is not present in /pkg/test/framework/features/features.yaml", f)
			t.goTest.FailNow()
			return nil
		}
		// feats actually contains feature and scenario.  split them here.
		onlyFeature := features.Feature(strings.Replace(string(f), scenario, "", 1))
		t.featureLabels[onlyFeature] = append(t.featureLabels[onlyFeature], scenario)
	}

	return t
}

func (t *testImpl) NotImplementedYet(features ...features.Feature) Test {
	t.notImplemented = true
	t.Features(features...).
		Run(func(_ TestContext) { t.goTest.Skip("Test Not Yet Implemented") })
	return t
}

func (t *testImpl) RequiresMinClusters(minClusters int) Test {
	t.requiredMinClusters = minClusters
	return t
}

func (t *testImpl) RequiresMaxClusters(maxClusters int) Test {
	t.requiredMaxClusters = maxClusters
	return t
}

func (t *testImpl) RequiresSingleCluster() Test {
	return t.RequiresMaxClusters(1).RequiresMinClusters(1)
}

func (t *testImpl) Run(fn func(ctx TestContext)) {
	t.runInternal(fn, false)
}

func (t *testImpl) RunParallel(fn func(ctx TestContext)) {
	t.runInternal(fn, true)
}

func (t *testImpl) runInternal(fn func(ctx TestContext), parallel bool) {
	// Disallow running the same test more than once.
	if t.ctx != nil {
		testName := t.name
		if testName == "" && t.goTest != nil {
			testName = t.goTest.Name()
		}
		panic(fmt.Sprintf("Attempting to run test `%s` more than once", testName))
	}

	if t.s.skipped {
		t.goTest.Skip("Skipped because parent Suite was skipped.")
		return
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

func (t *testImpl) doRun(ctx *testContext, fn func(ctx TestContext), parallel bool) {
	if fn == nil {
		panic("attempting to run test with nil function")
	}

	t.ctx = ctx

	if t.requiredMinClusters > 0 && len(t.s.Environment().Clusters()) < t.requiredMinClusters {
		ctx.Done()
		t.goTest.Skipf("Skipping %q: number of clusters %d is below required min %d",
			t.goTest.Name(), len(t.s.Environment().Clusters()), t.requiredMinClusters)
		return
	}

	if t.requiredMaxClusters > 0 && len(t.s.Environment().Clusters()) > t.requiredMaxClusters {
		ctx.Done()
		t.goTest.Skipf("Skipping %q: number of clusters %d is above required max %d",
			t.goTest.Name(), len(t.s.Environment().Clusters()), t.requiredMaxClusters)
		return
	}

	start := time.Now()

	scopes.Framework.Infof("=== BEGIN: Test: '%s[%s]' ===", rt.suiteContext().Settings().TestID, t.goTest.Name())

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

	defer func() {
		doneFn := func() {
			message := "passed"
			if t.goTest.Failed() {
				message = "failed"
			}
			end := time.Now()
			scopes.Framework.Infof("=== DONE (%s):  Test: '%s[%s] (%v)' ===",
				message,
				rt.suiteContext().Settings().TestID,
				t.goTest.Name(),
				end.Sub(start))
			rt.suiteContext().registerOutcome(t)
			ctx.Done()
			if t.hasParallelChildren {
				globalParentLock.Delete(t)
			}
		}
		if t.hasParallelChildren {
			// If a child is running in parallel, it won't continue until this test returns.
			// Since ctx.Done() will block until the child test is complete, we run ctx.Done()
			// asynchronously.
			globalParentLock.Store(t, struct{}{})
			go doneFn()
		} else {
			doneFn()
		}
	}()

	fn(ctx)
}
