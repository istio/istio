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

package locality

import (
	"fmt"
	"testing"
	"time"

	"istio.io/pkg/log"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
)

// This test allows for Locality Prioritized Load Balancing testing without needing Kube nodes in multiple regions.
// We do this by overriding the calling service's locality with the istio-locality label and the serving
// service's locality with a service entry.
//
// The created Service Entry for fake-(cds|eds)-external-service-12345.com points the domain at services
// that exist internally in the mesh. In the Service Entry we set service B to be in the same locality
// as the caller (A) and service C to be in a different locality. We then verify that all request go to
// service B. For details check the Service Entry configuration at the bottom of the page.
//
//  CDS Test
//
//                                                                 +-> b (region.zone.subzone)
//                                                                 |
//                                                            100% |
//                                                                 |
// A (region.zone.subzone) -> fake-cds-external-service-12345.com -|
//                                                                 |
//                                                              0% |
//                                                                 |
//                                                                 +-> c (notregion.notzone.notsubzone)
//
//
//  EDS Test
//
//                                                                 +-> 10.28.1.138 (b -> region.zone.subzone)
//                                                                 |
//                                                            100% |
//                                                                 |
// A (region.zone.subzone) -> fake-eds-external-service-12345.com -|
//                                                                 |
//                                                              0% |
//                                                                 |
//                                                                 +-> 10.28.1.139 (c -> notregion.notzone.notsubzone)

func TestPrioritized(t *testing.T) {
	framework.NewTest(t).
		RunParallel(func(ctx framework.TestContext) {

			ctx.NewSubTest("CDS").
				RunParallel(func(ctx framework.TestContext) {
					ns := namespace.NewOrFail(ctx, ctx, namespace.Config{
						Prefix: "locality-prioritized-cds",
						Inject: true,
					})

					var a, b, c echo.Instance
					echoboot.NewBuilderOrFail(ctx, ctx).
						With(&a, echoConfig(ns, "a")).
						With(&b, echoConfig(ns, "b")).
						With(&c, echoConfig(ns, "c")).
						BuildOrFail(ctx)

					fakeHostname := fmt.Sprintf("fake-cds-external-service-%v.com", r.Int())
					deploy(ctx, ctx, ns, serviceConfig{
						Name:             "prioritized-cds",
						Host:             fakeHostname,
						Namespace:        ns.Name(),
						Resolution:       "DNS",
						ServiceBAddress:  "b",
						ServiceBLocality: "region/zone/subzone",
						ServiceCAddress:  "c",
						ServiceCLocality: "notregion/notzone/notsubzone",
					}, a, failoverTemplate)

					// Send traffic to service B via a service entry.
					log.Infof("Sending traffic to local service (CDS) via %v", fakeHostname)
					if err := retry.UntilSuccess(func() error {
						return sendTraffic(a, fakeHostname, expectAllTrafficToB)
					}, retry.Delay(time.Second*5)); err != nil {
						ctx.Fatal(err)
					}
				})

			ctx.NewSubTest("EDS").
				RunParallel(func(ctx framework.TestContext) {

					ns := namespace.NewOrFail(ctx, ctx, namespace.Config{
						Prefix: "locality-prioritized-eds",
						Inject: true,
					})

					var a, b, c echo.Instance
					echoboot.NewBuilderOrFail(ctx, ctx).
						With(&a, echoConfig(ns, "a")).
						With(&b, echoConfig(ns, "b")).
						With(&c, echoConfig(ns, "c")).
						BuildOrFail(ctx)

					fakeHostname := fmt.Sprintf("fake-eds-external-service-%v.com", r.Int())

					deploy(ctx, ctx, ns, serviceConfig{
						Name:             "prioritized-eds",
						Host:             fakeHostname,
						Namespace:        ns.Name(),
						Resolution:       "STATIC",
						ServiceBAddress:  b.Address(),
						ServiceBLocality: "region/zone/subzone",
						ServiceCAddress:  c.Address(),
						ServiceCLocality: "notregion/notzone/notsubzone",
					}, a, failoverTemplate)

					// Send traffic to service B via a service entry.
					log.Infof("Sending traffic to local service (EDS) via %v", fakeHostname)
					if err := retry.UntilSuccess(func() error {
						return sendTraffic(a, fakeHostname, expectAllTrafficToB)
					}, retry.Delay(time.Second*5)); err != nil {
						ctx.Fatal(err)
					}
				})
		})
}
