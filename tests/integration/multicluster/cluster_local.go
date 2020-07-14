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

package multicluster

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/label"
)

// ClusterLocalTest tests that traffic works within a local cluster while in a multicluster configuration
// clusterLocalNS have been configured in meshConfig.serviceSettings to be clusterLocal.
func ClusterLocalTest(t *testing.T, clusterLocalNS namespace.Instance, feature features.Feature) {
	framework.NewTest(t).
		Features(feature).
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("respect-cluster-local-config").Run(func(ctx framework.TestContext) {
				clusters := ctx.Environment().Clusters()
				for i := range clusters {
					i := i
					ctx.NewSubTest(fmt.Sprintf("cluster-%d cluster local", i)).
						Label(label.Multicluster).
						RunParallel(func(ctx framework.TestContext) {
							local := clusters[i]

							// Deploy src only in local, but dst in all clusters. dst in remote clusters shouldn't be hit
							srcName, dstName := fmt.Sprintf("src-%d", i), fmt.Sprintf("dst-%d", i)
							var src, dst echo.Instance
							builder := echoboot.NewBuilderOrFail(ctx, ctx).
								With(&src, newEchoConfig(srcName, clusterLocalNS, local)).
								With(&dst, newEchoConfig(dstName, clusterLocalNS, local))
							for j, remoteCluster := range clusters {
								if i == j {
									continue
								}
								var ref echo.Instance
								builder = builder.With(&ref, newEchoConfig(dstName, clusterLocalNS, remoteCluster))
							}
							builder.BuildOrFail(ctx)

							results := callOrFail(ctx, src, dst)

							// Ensure that all requests went to the local cluster.
							results.CheckClusterOrFail(ctx, local.Name())
						})
				}
			})
		})
}
