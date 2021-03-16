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
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
)

type (
	perDeploymentTest func(ctx framework.TestContext, instances echo.Instances)
	perInstanceTest   func(ctx framework.TestContext, src echo.Instance, dst echo.Instances)
)

// Run will generate nested subtests for every instance in every deployment. The subtests will be nested including
// the source service, source cluster and destination deployment. Example: `a/to_b/from_cluster-0`
func (t *T) Run(testFn perInstanceTest) {
	t.fromEachDeployment(t.rootCtx, func(ctx framework.TestContext, srcInstances echo.Instances) {
		t.setup(ctx, srcInstances)
		t.toEachDeployment(ctx, func(ctx framework.TestContext, dstInstances echo.Instances) {
			t.setupPair(ctx, srcInstances, dstInstances)
			t.fromEachCluster(ctx, srcInstances, dstInstances, testFn)
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

func (t *T) fromEachCluster(ctx framework.TestContext, src, dst echo.Instances, testFn perInstanceTest) {
	for _, srcInstance := range src {
		srcInstance := srcInstance
		filteredDst := t.applyCombinationFilters(srcInstance, dst)
		if len(filteredDst) == 0 {
			// this only happens due to conditional filters and when an entire deployment is filtered we should be noisy
			ctx.Skipf("cases from %s in %s with %s as destination are removed by filters ",
				srcInstance.Config().Service, srcInstance.Config().Cluster.StableName(), dst[0].Config().Service)
			continue
		}
		if len(ctx.Clusters()) == 1 && len(src) == 1 {
			testFn(ctx, srcInstance, filteredDst)
		} else {
			ctx.NewSubTestf("from %s", srcInstance.Config().Cluster.StableName()).Run(func(ctx framework.TestContext) {
				testFn(ctx, srcInstance, filteredDst)
			})
		}

	}
}
