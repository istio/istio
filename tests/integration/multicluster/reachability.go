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
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/label"
)

const mcReachabilitySvcPerCluster = 3

// ReachabilityTest tests that services in 2 different clusters can talk to each other.
func ReachabilityTest(t *testing.T, ns namespace.Instance, pilots []pilot.Instance) {
	framework.NewTest(t).
		Label(label.Multicluster).
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("reachability").
				Run(func(ctx framework.TestContext) {
					// Deploy services in different clusters.
					// There are multiple instances in each cluster to tease out cases where remote proxies inconsistently
					// use different discovery servers (see https://github.com/istio/istio/issues/23591).
					clusters := ctx.Environment().Clusters()
					services := map[int][]echo.Instance{}
					builder := echoboot.NewBuilderOrFail(ctx, ctx)
					for i, cluster := range clusters {
						for j := 0; j < mcReachabilitySvcPerCluster; j++ {
							var instance echo.Instance
							svcName := fmt.Sprintf("echo-%d-%d", i, j)
							builder = builder.With(&instance, newEchoConfig(svcName, ns, cluster, pilots))
							services[i] = append(services[i], instance)
						}
					}
					builder.BuildOrFail(ctx)

					// Now verify that all services in each cluster can hit one service in each cluster.
					// Reaching 1 service per remote cluster makes the number linear rather than quadratic with
					// respect to len(clusters) * svcPerCluster.
					for _, srcServices := range services {
						for _, src := range srcServices {
							for _, dstServices := range services {
								src := src
								dest := dstServices[0]
								subTestName := fmt.Sprintf("%s->%s://%s:%s%s",
									src.Config().Service,
									"http",
									dest.Config().Service,
									"http",
									"/")

								ctx.NewSubTest(subTestName).
									RunParallel(func(ctx framework.TestContext) {
										_ = callOrFail(ctx, src, dest)
									})
							}
						}
					}
				})
		})
}
