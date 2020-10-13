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

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/label"
)

// ReachabilityTest tests that different services in 2 different clusters can talk to each other.
func ReachabilityTest(t *testing.T, apps AppContext, features ...features.Feature) {
	framework.NewTest(t).
		Label(label.Multicluster).
		Features(features...).
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("reachability").
				Run(func(ctx framework.TestContext) {
					clusters := ctx.Clusters()
					for _, src := range apps.UniqueEchos {
						for _, dstCluster := range clusters {
							src := src
							dest := apps.UniqueEchos.GetOrFail(ctx, echo.InCluster(dstCluster))
							subTestName := fmt.Sprintf("%s->%s://%s:%s%s",
								src.Config().Service,
								"http",
								dest.Config().Service,
								"http",
								"/")
							ctx.NewSubTest(subTestName).
								RunParallel(func(ctx framework.TestContext) {
									callOrFail(ctx, src, dest, nil)
								})
						}
					}
				})
		})
}
