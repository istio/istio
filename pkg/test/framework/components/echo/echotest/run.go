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

// Run will generate nested subtests from each instance in each deployment to each deployment
// For exampled, `a/to_b/from_cluster-0`. `a` is the source deployment, `b` is the destination deployment and `cluster-0`
// marks which instance of the source deployment
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

// RunToN will generate nested subtests for every instance in every deployment subsets of the full set of deployments,
// such that every deployment is a destination at least once. To create as few subtests as possible, the same deployment
// may appear as a target in multiple subtests.
//
// Example: Given a as the only source, with a, b, c, d as destinationsand n = 3, we get the following subtests:
//     a/to_a_b_c/from_cluster_1:
//     a/to_a_b_c/from_cluster_2:
//     a/to_b_c_d/from_cluster_1:
//     a/to_b_c_d/from_cluster_2:
//
func (t *T) RunToN(n int, testFn oneToNTest) {
	t.fromEachDeployment(t.rootCtx, func(ctx framework.TestContext, srcInstances echo.Instances) {
		t.setup(ctx, srcInstances)
		t.toNDeployments(ctx, n, srcInstances, func(ctx framework.TestContext, destDeployments []echo.Instances) {
			t.setupPair(ctx, srcInstances, destDeployments)
			t.fromEachCluster(ctx, srcInstances, func(ctx framework.TestContext, src echo.Instance) {
				testFn(ctx, src, destDeployments)
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

func (t *T) toNDeployments(ctx framework.TestContext, n int, srcs echo.Instances, testFn perNDeploymentTest) {
	// every eligible target deployment
	var filteredTargets []echo.Instances
	var commonTargets string
	for _, src := range srcs {
		filteredTargets = t.applyCombinationFilters(src, t.destinations).Deployments()
		targetNames := strings.Join(targetNames(filteredTargets), ";")
		if commonTargets == "" {
			commonTargets = targetNames
		} else if commonTargets != targetNames {
			ctx.Fatalf("%s in each cluster each cluster would not target the same set of deploments", src.Config().Service)
		}
	}

	for _, set := range nDestinations(ctx, n, filteredTargets) {
		set := set
		ctx.NewSubTestf("to %s", strings.Join(targetNames(set), " ")).Run(func(ctx framework.TestContext) {
			testFn(ctx, set)
		})
	}
}

func nDestinations(ctx framework.TestContext, n int, dests []echo.Instances) (out [][]echo.Instances) {
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
		out = append(out, dests[start:start+n])
	}
	return
}

func targetNames(deployments []echo.Instances) []string {
	var names []string
	for _, dest := range deployments {
		names = append(names, dest[0].Config().Service)
	}
	return names
}
