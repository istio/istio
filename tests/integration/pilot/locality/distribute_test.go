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
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

// This test allows for Locality Distribution Load Balancing testing without needing Kube nodes in multiple regions.
// We do this by overriding the calling service's locality with the istio-locality label and the serving
// service's locality with a service entry.
//
// The created Service Entry for fake-(cds|eds)-external-service-12345.com points the domain at services
// that exist internally in the mesh. In the Service Entry we set service B to be in the same locality
// as the caller (A) and service C to be in a different locality. We then verify that all request go to
// service B. For details check the Service Entry configuration at the bottom of the page.
//
//                                                                 +-> 10.28.1.138 (b -> region.zone.subzone)
//                                                                 |
//                                                            20% |
//                                                                 |
// A (region.zone.subzone) -> fake-eds-external-service-12345.com -|
//                                                                 |
//                                                              80% |
//                                                                 |
//                                                                 +-> 10.28.1.139 (c -> notregion.notzone.notsubzone)

func TestDistribute(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(ctx, ctx, namespace.Config{
				Prefix: "locality-distribute-eds",
				Inject: true,
			})

			var a, b, c echo.Instance
			echoboot.NewBuilderOrFail(ctx, ctx).
				With(&a, echoConfig(ns, "a")).
				With(&b, echoConfig(ns, "b")).
				With(&c, echoConfig(ns, "c")).
				BuildOrFail(ctx)

			fakeHostname := fmt.Sprintf("fake-eds-external-service-%v.com", r.Int())

			// First, deploy across multiple zones
			deploy(ctx, ctx, ns, serviceConfig{
				Name:       "distribute-eds",
				Host:       fakeHostname,
				Namespace:  ns.Name(),
				Resolution: "STATIC",
				// Add a fake service with no locality to ensure we don't route there
				NonExistantService:         "1.1.1.1",
				NonExistantServiceLocality: "",
				ServiceBAddress:            b.Address(),
				ServiceBLocality:           "region/zone/subzone",
				ServiceCAddress:            c.Address(),
				ServiceCLocality:           "notregion/notzone/notsubzone",
			}, a, distributeTemplate)

			log.Infof("Sending traffic to local service (EDS) via %v", fakeHostname)

			if err := retry.UntilSuccess(func() error {
				e := sendTraffic(a, fakeHostname, map[string]int{
					"b": 10,
					"c": 40,
				})
				scopes.Framework.Errorf("%v: ", e)
				return e
			}, retry.Timeout(time.Second*5)); err != nil {
				ctx.Fatal(err)
			}

			// Set a to no locality, b to matching locality, and c to non-matching.
			// Expect all to get even traffic after disabling locality lb.
			deploy(ctx, ctx, ns, serviceConfig{
				Name:                       "distribute-eds",
				Host:                       fakeHostname,
				Namespace:                  ns.Name(),
				Resolution:                 "STATIC",
				NonExistantService:         a.Address(),
				NonExistantServiceLocality: "",
				ServiceBAddress:            b.Address(),
				ServiceBLocality:           "region/zone/subzone",
				ServiceCAddress:            c.Address(),
				ServiceCLocality:           "notregion/notzone/notsubzone",
			}, a, disabledTemplate)

			log.Infof("Sending without locality enabled traffic to local service (EDS) via %v", fakeHostname)

			if err := retry.UntilSuccess(func() error {
				e := sendTraffic(a, fakeHostname, map[string]int{
					"a": 17,
					"b": 17,
					"c": 17,
				})
				scopes.Framework.Errorf("%v: ", e)
				return e
			}, retry.Delay(time.Second*5)); err != nil {
				ctx.Fatal(err)
			}
		})
}
