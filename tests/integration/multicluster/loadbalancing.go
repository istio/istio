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

	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/label"
)

func LoadbalancingTest(t *testing.T, apps AppContext, features ...features.Feature) {
	framework.NewTest(t).
		Label(label.Multicluster).
		Features(features...).
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("loadbalancing").
				Run(func(ctx framework.TestContext) {
					for _, src := range apps.LBEchos {
						src := src
						ctx.NewSubTest(fmt.Sprintf("from %s", src.Config().Cluster.Name())).
							Run(func(ctx framework.TestContext) {
								srcNetwork := src.Config().Cluster.NetworkName()
								res := callOrFail(ctx, src, apps.LBEchos[0])

								// make sure we reached all clusters, including cross-network
								if err := res.CheckReachedClusters(ctx.Clusters()); err != nil {
									ctx.Error(err)
								}

								// expect same network traffic to have very equal distribution (20% error)
								intraNetworkClusters := ctx.Clusters().ByNetwork()[srcNetwork]
								intraNetworkRes := res.Match(func(r *client.ParsedResponse) bool {
									return srcNetwork == ctx.Clusters().GetByName(r.Cluster).NetworkName()
								})
								if err := intraNetworkRes.CheckEqualClusterTraffic(intraNetworkClusters, 20); err != nil {
									ctx.Errorf("same network traffic was not even: %v", err)
								}
							})
					}
				})
		})
}
