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
	"github.com/hashicorp/go-multierror"
	"testing"

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
								res := callOrFail(ctx, src, apps.LBEchos[0])
								// verify we reached all instances by using ParsedResponse
								clusterHits := map[string]int{}
								for _, r := range res {
									clusterHits[r.Cluster]++
								}
								expected := len(res) / len(ctx.Clusters())
								var err *multierror.Error
								for _, c := range ctx.Clusters() {
									hits := clusterHits[c.Name()]
									if !almostEquals(hits, expected, expected/10) {
										err = multierror.Append(fmt.Errorf(
											"expected ~%d hits for %s, but got: %d",
											expected,
											c.Name(),
											hits,
										))
									}
								}
								if err := err.ErrorOrNil(); err != nil {
									ctx.Fatal(err)
								}
							})
					}
				})
		})
}

func almostEquals(a, b, precision int) bool {
	upper := a + precision
	lower := a - precision
	if b < lower || b > upper {
		return false
	}
	return true
}
