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

package echotest

import (
	"strings"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
)

type (
	perDeploymentTest  func(t framework.TestContext, instances echo.Instances)
	perNDeploymentTest func(t framework.TestContext, deployments []echo.Instances)
	perInstanceTest    func(t framework.TestContext, inst echo.Instance)

	oneToOneTest func(t framework.TestContext, src echo.Instance, dst echo.Instances)
	oneToNTest   func(t framework.TestContext, src echo.Instance, dsts []echo.Instances)
)

// Run will generate nested subtests for every instance in every deployment. The subtests will be nested including
// the source service, source cluster and destination deployment. Example: `a/to_b/from_cluster-0`
func (t *T) Run(testFn oneToOneTest) {
	t.fromEachDeployment(t.rootCtx, func(ctx framework.TestContext, srcInstances echo.Instances) {
		t.setup(ctx, srcInstances)
		t.toEachDeployment(ctx, func(ctx framework.TestContext, dstInstances echo.Instances) {
			t.setupPair(ctx, srcInstances, []echo.Instances{dstInstances})
			t.fromEachCluster(ctx, srcInstances, func(ctx framework.TestContext, src echo.Instance) {
				filteredDst := t.applyCombinationFilters(src, dstInstances)
				if len(filteredDst) == 0 {
					// this only happens due to conditional filters and when an entire deployment is filtered we should be noisy
					ctx.Skipf("cases from %s in %s with %s as destination are removed by filters ",
						src.Config().Service, src.Config().Cluster.StableName(), dstInstances[0].Config().Service)
				}
				testFn(ctx, src, filteredDst)
			})
		})
	})
}

func (t *T) RunToN(n int, testFn oneToNTest) {
	t.fromEachDeployment(t.rootCtx, func(ctx framework.TestContext, srcInstances echo.Instances) {
		t.setup(ctx, srcInstances)
		t.toNDeployments(ctx, n, func(ctx framework.TestContext, dstDeployments []echo.Instances) {
			t.setupPair(ctx, srcInstances, dstDeployments)
			t.fromEachCluster(ctx, srcInstances, func(ctx framework.TestContext, src echo.Instance) {
				filteredDst := make([]echo.Instances, 3)
				for i, dstDeployment := range dstDeployments {
					filteredDst[i] = t.applyCombinationFilters(src, dstDeployment)
					// TODO rather than skipping for 1/n being ineligible, can we filter earlier and avoid calling setupPair for each cluster?
					if len(filteredDst[i]) == 0 {
						// this only happens due to conditional filters and when an entire deployment is filtered we should be noisy
						ctx.Skipf("cases from %s in %s with %s as destination are removed by filters ",
							src.Config().Service, src.Config().Cluster.StableName(), dstDeployment[0].Config().Service)
					}
				}
				testFn(ctx, src, dstDeployments)
			})
		})
	})
}

// fromEachDeployment enumerates subtests for deployment with the structure <src>
// Intended to be used in combination with other helpers to enumerate subtests for destinations.
func (t *T) fromEachDeployment(ctx framework.TestContext, testFn perDeploymentTest) {
	for _, src := range t.sources.Deployments() {
		src := src
		ctx.NewSubTestf("%s", src[0].Config().Service).Run(func(ctx framework.TestContext) {
			testFn(ctx, src)
		})
	}
}

// toEachDeployment enumerates subtests for every deployment as a destination, adding /to_<dst> to the parent test.
// Intended to be used in combination with other helpers which enumerates the subtests and chooses the srcInstnace.
func (t *T) toEachDeployment(ctx framework.TestContext, testFn perDeploymentTest) {
	for _, dst := range t.destinations.Deployments() {
		dst := dst
		ctx.NewSubTestf("to %s", dst[0].Config().Service).Run(func(ctx framework.TestContext) {
			testFn(ctx, dst)
		})
	}
}

func (t *T) fromEachCluster(ctx framework.TestContext, src echo.Instances, testFn perInstanceTest) {
	for _, srcInstance := range src {
		srcInstance := srcInstance
		if len(ctx.Clusters()) == 1 && len(src) == 1 {
			testFn(ctx, srcInstance)
		} else {
			ctx.NewSubTestf("from %s", srcInstance.Config().Cluster.StableName()).Run(func(ctx framework.TestContext) {
				testFn(ctx, srcInstance)
			})
		}
	}
}

func (t *T) toNDeployments(ctx framework.TestContext, n int, testFn perNDeploymentTest) {
	dests := t.destinations.Deployments()
	nDests := len(dests)
	if nDests < n {
		ctx.Fatalf("want to run with %d destinations but there are only %d total", n, nDests)
	}

	for i := 0; i < nDests; i += n {
		start := i
		if start+n-1 >= nDests {
			// re-use a few destinations to fit the entire slice in
			start = nDests - n
		}
		testDests := dests[start : start+n]
		var names []string
		for _, dest := range testDests {
			names = append(names, dest[0].Config().Service)
		}
		ctx.NewSubTestf("to %s", strings.Join(names, " ")).Run(func(ctx framework.TestContext) {
			testFn(ctx, testDests)
		})
	}
}
