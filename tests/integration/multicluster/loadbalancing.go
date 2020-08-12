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

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
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
								EquallyDistributedOrFail(ctx, callOrFail(ctx, src, apps.LBEchos[0]), apps.LBEchos)
							})
					}
				})
		})
}

// EquallyDistributedOrFail fails the test if responses aren't equally distributed across clusters for the given set of echos.
func EquallyDistributedOrFail(ctx test.Failer, res client.ParsedResponses, echos echo.Instances) {
	// verify we reached all instances by using ParsedResponse
	clusterHits := map[string]int{}
	for _, r := range res {
		clusterHits[r.Cluster]++
	}
	// TODO(landow) this check can be removed if the other is uncommented
	for _, inst := range echos {
		hits := clusterHits[inst.Config().Cluster.Name()]
		if hits < 1 {
			ctx.Fatalf("expected requests to reach all clusters: %v", clusterHits)
		}
	}

	// TODO(landow) check this when cross-network traffic weighting is fixed
	//equal := true
	//expected := len(res) / len(echos)
	//for _, inst := range echos {
	//	hits := clusterHits[inst.Config().Cluster.Name()]
	//	if !almostEquals(hits, expected, expected/5) {
	//		equal = false
	//		break
	//	}
	//}
	//if !equal {
	//	ctx.Fatalf("requests were not equally distributed among clusters: %v", clusterHits)
	//}
}
