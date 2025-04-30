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
	context2 "context"
	"fmt"
	"testing"
	"time"

	traceapi "go.opentelemetry.io/otel/trace"

	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/tracing"
)

type Test interface {
	// Label applies the given labels to this test.
	Label(labels ...label.Instance) Test
	// RequireIstioVersion ensures that all installed versions of Istio are at least the
	// required version for the annotated test to pass
	RequireIstioVersion(version string) Test
	// RequireKubernetesMinorVersion ensures that all Kubernetes clusters used in this test
	// are at least the required version for the annotated test to pass
	RequireKubernetesMinorVersion(minorVersion uint) Test
	// RequiresMinClusters ensures that the current environment contains at least the expected number of clusters.
	// Otherwise it stops test execution and skips the test.
	//
	// Deprecated: Tests should not make assumptions about number of clusters.
	RequiresMinClusters(minClusters int) Test
	// RequiresSingleCluster this a utility that requires the min/max clusters to both = 1.
	//
	// Deprecated: All new tests should support multiple clusters.
	RequiresSingleCluster() Test
	// RequiresDualstack ensures that test context contains DualStack configuration.
	RequiresDualStack() Test
	// RequiresLocalControlPlane ensures that clusters are using locally-deployed control planes.
	//
	// Deprecated: Tests should not make assumptions regarding control plane topology.
	RequiresLocalControlPlane() Test
	// RequiresSingleNetwork ensures that clusters are in the same network
	//
	// Deprecated: Tests should not make assumptions regarding number of networks.
	RequiresSingleNetwork() Test
	// TopLevel marks a test as a "top-level test" meaning a container test that has many subtests.
	// Resources created at this level will be in-scope for dumping when any descendant test fails.
	TopLevel() Test
	// Run the test, supplied as a lambda.
	Run(fn func(t TestContext))
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
	RunParallel(fn func(t TestContext))
}

// Test allows the test author to specify test-related metadata in a fluent-style, before commencing execution.
type testImpl struct {
	// name to be used when creating a Golang test. Only used for subtests.
	name                      string
	parent                    *testImpl
	goTest                    *testing.T
	labels                    []label.Instance
	s                         *suiteContext
	requiredMinClusters       int
	requiredMaxClusters       int
	requireLocalIstiod        bool
	requireSingleNetwork      bool
	requiredDualstack         bool
	minIstioVersion           string
	minKubernetesMinorVersion uint
	topLevel                  bool

	ctx *testContext
	tc  context2.Context
	ts  traceapi.Span
}

// NewTest returns a new test wrapper for running a single test.
func NewTest(t *testing.T) Test {
	rtMu.Lock()
	defer rtMu.Unlock()

	if rt == nil {
		panic("call to scope without running the test framework")
	}

	ctx, span := tracing.Start(rt.suiteContext().traceContext, t.Name())

	runner := &testImpl{
		tc:     ctx,
		ts:     span,
		s:      rt.suiteContext(),
		goTest: t,
	}

	return runner
}

func (t *testImpl) Label(labels ...label.Instance) Test {
	t.labels = append(t.labels, labels...)
	return t
}

func (t *testImpl) TopLevel() Test {
	t.topLevel = true
	return t
}

func (t *testImpl) RequiresMinClusters(minClusters int) Test {
	t.requiredMinClusters = minClusters
	return t
}

func (t *testImpl) RequiresSingleCluster() Test {
	t.requiredMaxClusters = 1
	// nolint: staticcheck
	return t.RequiresMinClusters(1)
}

func (t *testImpl) RequiresDualStack() Test {
	t.requiredDualstack = true
	// nolint: staticcheck
	return t
}

func (t *testImpl) RequiresLocalControlPlane() Test {
	t.requireLocalIstiod = true
	return t
}

func (t *testImpl) RequiresSingleNetwork() Test {
	t.requireSingleNetwork = true
	return t
}

func (t *testImpl) RequireIstioVersion(version string) Test {
	t.minIstioVersion = version
	return t
}

func (t *testImpl) RequireKubernetesMinorVersion(minorVersion uint) Test {
	t.minKubernetesMinorVersion = minorVersion
	return t
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

	// we check kube for min clusters, these assume we're talking about real multicluster.
	// it's possible to have 1 kube cluster then 1 non-kube cluster (vm for example)
	if t.requiredMinClusters > 0 && len(t.s.Environment().Clusters()) < t.requiredMinClusters {
		t.goTest.Skipf("Skipping %q: number of clusters %d is below required min %d",
			t.goTest.Name(), len(t.s.Environment().Clusters()), t.requiredMinClusters)
		return
	}

	// max clusters doesn't check kube only, the test may be written in a way that doesn't loop over all of Clusters()
	if t.requiredMaxClusters > 0 && len(t.s.Environment().Clusters()) > t.requiredMaxClusters {
		t.goTest.Skipf("Skipping %q: number of clusters %d is above required max %d",
			t.goTest.Name(), len(t.s.Environment().Clusters()), t.requiredMaxClusters)
		return
	}

	if t.requiredDualstack && len(t.ctx.Settings().IPFamilies) < 2 {
		t.goTest.Skipf("Skipping %q: context does not have DualStack configuration",
			t.goTest.Name())
	}
	if t.minKubernetesMinorVersion > 0 {
		for _, c := range ctx.Clusters() {
			if !c.MinKubeVersion(t.minKubernetesMinorVersion) {
				t.goTest.Skipf("Skipping %q: cluster %s is below required min k8s version 1.%d",
					t.goTest.Name(), c.Name(), t.minKubernetesMinorVersion)
				return
			}
		}
	}

	if t.requireLocalIstiod {
		for _, c := range ctx.Clusters() {
			if !c.IsPrimary() {
				t.goTest.Skipf("Skipping %q: cluster %s is not using a local control plane", t.goTest.Name(), c.Name())

				return
			}
		}
	}

	if t.requireSingleNetwork && t.s.Environment().IsMultiNetwork() {
		t.goTest.Skipf("Skipping %q: only single network allowed", t.goTest.Name())

		return
	}

	if t.minIstioVersion != "" {
		if !t.ctx.Settings().Revisions.AtLeast(resource.IstioVersion(t.minIstioVersion)) {
			t.goTest.Skipf("Skipping %q: running with min Istio version %q, test requires at least %s",
				t.goTest.Name(), t.ctx.Settings().Revisions.Minimum(), t.minIstioVersion)
		}
	}

	start := time.Now()
	scopes.Framework.Infof("=== BEGIN: Test: '%s[%s]' ===", rt.suiteContext().Settings().TestID, t.goTest.Name())

	// Initial setup if we're running in Parallel.
	if parallel {
		// Run the underlying Go test in parallel. This will not return until the parent
		// test (if there is one) exits.
		t.goTest.Parallel()
	}

	// Register the cleanup function for when the Go test completes.
	t.goTest.Cleanup(func() {
		message := "passed"
		if t.goTest.Failed() {
			message = "failed"
		}
		if t.goTest.Skipped() {
			message = "skipped"
		}
		scopes.Framework.Infof("=== DONE (%s):  Test: '%s[%s] (%v)' ===",
			message,
			rt.suiteContext().Settings().TestID,
			t.goTest.Name(),
			time.Since(start))
		t.ts.End()
	})

	// Run the user's test function.
	fn(ctx)
}
