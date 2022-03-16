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

	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
)

type (
	perDeploymentTest  func(t framework.TestContext, deployments echo.Instances)
	perNDeploymentTest func(t framework.TestContext, deployments echo.Services)
	perInstanceTest    func(t framework.TestContext, inst echo.Instance)
	perClusterTest     func(t framework.TestContext, c cluster.Cluster)

	oneToOneTest      func(t framework.TestContext, from echo.Instance, to echo.Target)
	oneToNTest        func(t framework.TestContext, from echo.Instance, dsts echo.Services)
	oneClusterOneTest func(t framework.TestContext, from cluster.Cluster, to echo.Target)
	ingressTest       func(t framework.TestContext, from ingress.Instance, to echo.Target)
)

// Run will generate and run one subtest to send traffic between each combination
// of source instance to target deployment.
//
// For example, in a test named `a/to_b/from_cluster-0`,
// `a` is the source deployment, `b` is the destination deployment and
// `cluster-0` marks which instance of the source deployment.
//
// We use a combination of deployment name and cluster name to identify
// a particular source instance, as there should typically be one instance
// per cluster for any deployment. However we do not identify a destination
// cluster, as we expect most tests will cause load-balancing across all possible
// clusters.
func (t *T) Run(testFn oneToOneTest) {
	t.rootCtx.Logf("Running tests with: sources %v -> destinations %v",
		t.sources.Services().ServiceNamesWithNamespacePrefix(),
		t.destinations.Services().ServiceNamesWithNamespacePrefix())
	t.fromEachDeployment(t.rootCtx, func(ctx framework.TestContext, from echo.Instances) {
		t.setup(ctx, from.Callers())
		t.toEachDeployment(ctx, func(ctx framework.TestContext, to echo.Instances) {
			t.setupPair(ctx, from.Callers(), echo.Services{to})
			t.fromEachWorkloadCluster(ctx, from, func(ctx framework.TestContext, from echo.Instance) {
				filteredDst := t.applyCombinationFilters(from, to)
				if len(filteredDst) == 0 {
					// this only happens due to conditional filters and when an entire deployment is filtered we should be noisy
					ctx.Skipf("cases from %s in %s with %s as destination are removed by filters ",
						from.Config().Service, from.Config().Cluster.StableName(), to[0].Config().Service)
				}
				testFn(ctx, from, filteredDst)
			})
		})
	})
}

// RunFromClusters will generate and run one subtest to send traffic to
// destination instance. This is for ingress gateway testing when source instance
// destination instances. This can be used when we're not using echo workloads
// as the source of traffic, such as from the ingress gateway. For example:
//
//    RunFromClusters(func(t framework.TestContext, src cluster.Cluster, dst echo.Instances)) {
//      ingr := ist.IngressFor(src)
//      ingr.CallWithRetryOrFail(...)
//    })
func (t *T) RunFromClusters(testFn oneClusterOneTest) {
	t.toEachDeployment(t.rootCtx, func(ctx framework.TestContext, dstInstances echo.Instances) {
		t.setupPair(ctx, nil, echo.Services{dstInstances})
		if len(ctx.Clusters()) == 1 {
			testFn(ctx, ctx.Clusters()[0], dstInstances)
		} else {
			t.fromEachCluster(ctx, func(ctx framework.TestContext, c cluster.Cluster) {
				testFn(ctx, c, dstInstances)
			})
		}
	})
}

// fromEachCluster runs test from each cluster without requiring source deployment.
func (t *T) fromEachCluster(ctx framework.TestContext, testFn perClusterTest) {
	for _, fromCluster := range t.sources.Clusters() {
		fromCluster := fromCluster
		ctx.NewSubTestf("from %s", fromCluster.StableName()).Run(func(ctx framework.TestContext) {
			testFn(ctx, fromCluster)
		})
	}
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
	t.fromEachDeployment(t.rootCtx, func(ctx framework.TestContext, from echo.Instances) {
		t.setup(ctx, from.Callers())
		t.toNDeployments(ctx, n, from, func(ctx framework.TestContext, toServices echo.Services) {
			t.setupPair(ctx, from.Callers(), toServices)
			t.fromEachWorkloadCluster(ctx, from, func(ctx framework.TestContext, fromInstance echo.Instance) {
				// reapply destination filters to only get the reachable instances for this cluster
				// this can be done safely since toNDeployments asserts the Services won't change
				destDeployments := t.applyCombinationFilters(fromInstance, toServices.Instances()).Services()
				testFn(ctx, fromInstance, destDeployments)
			})
		})
	})
}

func (t *T) RunViaIngress(testFn ingressTest) {
	i := istio.GetOrFail(t.rootCtx, t.rootCtx)
	t.toEachDeployment(t.rootCtx, func(ctx framework.TestContext, dstInstances echo.Instances) {
		t.setupPair(ctx, i.Ingresses().Callers(), echo.Services{dstInstances})
		doTest := func(ctx framework.TestContext, fromCluster cluster.Cluster, dst echo.Instances) {
			ingr := i.IngressFor(fromCluster)
			if ingr == nil {
				ctx.Skipf("no ingress for %s", fromCluster.StableName())
			}
			testFn(ctx, ingr, dst)
		}
		if len(ctx.Clusters()) == 1 {
			doTest(ctx, ctx.Clusters()[0], dstInstances)
		} else {
			t.fromEachCluster(ctx, func(ctx framework.TestContext, c cluster.Cluster) {
				doTest(ctx, c, dstInstances)
			})
		}
	})
}

func (t *T) isMultipleNamespaces() bool {
	namespaces := map[string]struct{}{}
	for _, instances := range []echo.Instances{t.sources, t.destinations} {
		for _, i := range instances {
			namespaces[i.Config().Namespace.Name()] = struct{}{}
			if len(namespaces) > 1 {
				return true
			}
		}
	}
	return false
}

// fromEachDeployment enumerates subtests for deployment with the structure <src>
// Intended to be used in combination with other helpers to enumerate subtests for destinations.
func (t *T) fromEachDeployment(ctx framework.TestContext, testFn perDeploymentTest) {
	includeNS := t.isMultipleNamespaces()

	for _, from := range t.sources.Services() {
		from := from
		subtestName := from.Config().Service
		if includeNS {
			subtestName += "." + from.Config().Namespace.Prefix()
		}
		ctx.NewSubTest(subtestName).Run(func(ctx framework.TestContext) {
			testFn(ctx, from)
		})
	}
}

// toEachDeployment enumerates subtests for every deployment as a destination, adding /to_<dst> to the parent test.
// Intended to be used in combination with other helpers which enumerates the subtests and chooses the source instances.
func (t *T) toEachDeployment(ctx framework.TestContext, testFn perDeploymentTest) {
	includeNS := t.isMultipleNamespaces()

	for _, to := range t.destinations.Services() {
		to := to
		subtestName := to.Config().Service
		if includeNS {
			subtestName += "." + to.Config().Namespace.Prefix()
		}
		ctx.NewSubTestf("to %s", subtestName).Run(func(ctx framework.TestContext) {
			testFn(ctx, to)
		})
	}
}

func (t *T) fromEachWorkloadCluster(ctx framework.TestContext, from echo.Instances, testFn perInstanceTest) {
	for _, fromInstance := range from {
		fromInstance := fromInstance
		if len(ctx.Clusters()) == 1 && len(from) == 1 {
			testFn(ctx, fromInstance)
		} else {
			ctx.NewSubTestf("from %s", fromInstance.Config().Cluster.StableName()).Run(func(ctx framework.TestContext) {
				// assumes we don't change config from cluster to cluster
				ctx.SkipDumping()
				testFn(ctx, fromInstance)
			})
		}
	}
}

func (t *T) toNDeployments(ctx framework.TestContext, n int, from echo.Instances, testFn perNDeploymentTest) {
	includeNS := t.isMultipleNamespaces()

	// every eligible target instance from any cluster (map to dedupe)
	var commonTargets []string
	for _, fromInstance := range from {
		// eligible target instances from the source cluster
		filteredForSource := t.applyCombinationFilters(fromInstance, t.destinations)
		// make sure this targets the same deployments (not necessarily the same instances)
		targetNames := filteredForSource.Services().FQDNs()
		if len(commonTargets) == 0 {
			commonTargets = targetNames
		} else if !util.StringSliceEqual(targetNames, commonTargets) {
			ctx.Fatalf("%s in each cluster each cluster would not target the same set of deploments", fromInstance.Config().Service)
		}
	}

	// we take all instances that match the deployments
	// combination filters should be run again for individual sources
	filteredTargets := t.destinations.Services().MatchFQDNs(commonTargets...)
	for _, set := range nDestinations(ctx, n, filteredTargets) {
		set := set

		namespacedNames := set.ServiceNamesWithNamespacePrefix()
		var toNames []string
		if includeNS {
			toNames = namespacedNames.NamespacedNames()
		} else {
			toNames = namespacedNames.Names()
		}

		ctx.NewSubTestf("to %s", strings.Join(toNames, " ")).Run(func(ctx framework.TestContext) {
			testFn(ctx, set)
		})
	}
}

// nDestinations splits the given deployments into subsets of size n. A deployment may be present in multiple subsets to
// ensure every deployment is included.
func nDestinations(ctx framework.TestContext, n int, deployments echo.Services) (out []echo.Services) {
	nDests := len(deployments)
	if nDests < n {
		ctx.Fatalf("want to run with %d destinations but there are only %d total", n, nDests)
	}
	for i := 0; i < nDests; i += n {
		start := i
		if start+n-1 >= nDests {
			// re-use a few destinations to fit the entire slice in
			start = nDests - n
		}
		out = append(out, deployments[start:start+n])
	}
	return
}
