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
					ctx.NewSubTest(fmt.Sprintf("from %s", apps.LBEchos[0].Config().Cluster.Name())).
						Run(func(ctx framework.TestContext) {
							res := callOrFail(ctx, apps.LBEchos[0], apps.LBEchos[0])
							if err := res.CheckEqualClusterTraffic(); err != nil {
								ctx.Fatal(err)
							}
						})
				})
		})
}
