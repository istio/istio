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

// ReachabilityTest tests that services in 2 different clusters can talk to each other.
func ReachabilityTest(t *testing.T, pilots []pilot.Instance) {
	framework.NewTest(t).
		Label(label.Multicluster).
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("reachability").
				Run(func(ctx framework.TestContext) {
					ns := namespace.NewOrFail(ctx, ctx, namespace.Config{
						Prefix: "mc-reachability",
						Inject: true,
					})

					// Deploy services in different clusters.
					// There are more in the remote cluster to tease out cases where remote proxies inconsistently
					// use different discovery servers.
					var a, b, c, d, e, f echo.Instance
					echoboot.NewBuilderOrFail(ctx, ctx).
						With(&a, newEchoConfig("a", ns, ctx.Environment().Clusters()[0], pilots)).
						With(&b, newEchoConfig("b", ns, ctx.Environment().Clusters()[1], pilots)).
						With(&c, newEchoConfig("c", ns, ctx.Environment().Clusters()[1], pilots)).
						With(&d, newEchoConfig("d", ns, ctx.Environment().Clusters()[1], pilots)).
						With(&e, newEchoConfig("e", ns, ctx.Environment().Clusters()[1], pilots)).
						With(&f, newEchoConfig("f", ns, ctx.Environment().Clusters()[1], pilots)).
						BuildOrFail(ctx)

					// Now verify that they can talk to each other.
					for _, src := range []echo.Instance{a, b, c, d, e, f} {
						for _, dest := range []echo.Instance{a, b} {
							src := src
							dest := dest
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
				})
		})
}
