// +build integ
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
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
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
								callOrFail(ctx, src, apps.LBEchos[0],
									checkReachedAllSubsets(apps.LBEchos),
									checkEqualIntraNetworkTraffic(ctx.Clusters(), srcNetwork))
							})
					}
				})
		})
}

func checkReachedAllSubsets(echos echo.Instances) callChecker {
	return func(res client.ParsedResponses) error {
		// make sure we reached all cluster/subset combos
		for _, e := range echos {
			for _, ss := range e.Config().Subsets {
				version, cluster := ss.Version, e.Config().Cluster.Name()
				responses := res.Match(func(r *client.ParsedResponse) bool {
					return r.Cluster == cluster && r.Version == version
				})
				if len(responses) < 1 {
					return fmt.Errorf("did not reach %s in %s", version, cluster)
				}
			}
		}
		return nil
	}
}

func checkEqualIntraNetworkTraffic(clusters resource.Clusters, srcNetwork string) callChecker {
	// expect same network traffic to have very equal distribution (20% error)
	intraNetworkClusters := clusters.ByNetwork()[srcNetwork]
	return func(res client.ParsedResponses) error {
		intraNetworkRes := res.Match(func(r *client.ParsedResponse) bool {
			return srcNetwork == clusters.GetByName(r.Cluster).NetworkName()
		})
		if err := intraNetworkRes.CheckEqualClusterTraffic(intraNetworkClusters, 20); err != nil {
			return fmt.Errorf("same network traffic was not even: %v", err)
		}
		return nil
	}
}
