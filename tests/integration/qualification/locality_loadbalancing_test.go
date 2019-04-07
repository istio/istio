// Copyright 2019 Istio Authors
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

package qualification

import (
	"net/http"
	"regexp"
	"sync"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/apps"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/pilot"
)

// This test allows for Locality Load Balancing testing without needing Kube nodes in multiple regions.
// We do this by overriding the calling service locality with the istio-locality label and the serving
// services with a service entry.
//
// The created Service Entry for fake-external-service.com points the domain at services
// that exist internally in the mesh. In the Service Entry we set service B to be in the same locality
// as the caller (A) and service C to be in a different locality. We then verify that all request go to
// service B. For details check the Service Entry configuration at the bottom of the page.
//
//
//                                                       +-> B (region.zone.subzone)
//                                                       |
//                                                  100% |
//                                                       |
// A (region.zone.subzone) -> fake-external-service.com -|
//                                                       |
//                                                    0% |
//                                                       |
//                                                       +-> C (notregion.notzone.notsubzone)

var serviceBHostname = regexp.MustCompile("^b-.*$")

func TestLocalityLoadBalancingEDS(t *testing.T) {
	ctx := framework.NewContext(t)
	defer ctx.Done(t)

	ctx.RequireOrSkip(t, environment.Kube)

	g := galley.NewOrFail(t, ctx, galley.Config{})
	p := pilot.NewOrFail(t, ctx, pilot.Config{Galley: g})

	instance := apps.NewOrFail(ctx, t, apps.Config{Pilot: p})
	a := instance.GetAppOrFail("a", t).(apps.KubeApp)
	g.ApplyConfigOrFail(t, instance.Namespace(), fakeExternalServiceConfig)

	// TODO: find a better way to do this!
	// This sleep allows config to propagate
	time.Sleep(10 * time.Second)

	// Send traffic to service B via a service entry for the test duration.
	wg := &sync.WaitGroup{}
	wg.Add(numSendTasks + 1)
	go logProgress(20*time.Second, wg, "fake-external-service.com")
	for i := 0; i < numSendTasks; i++ {
		go sendTraffic(t, testDuration, a, "fake-external-service.com", wg)
	}
	wg.Wait()

}

func sendTraffic(t *testing.T, duration time.Duration, from apps.KubeApp, to string, wg *sync.WaitGroup) {
	timeout := time.After(duration)
	for {
		select {
		case <-timeout:
			wg.Done()
			return
		default:
			headers := http.Header{}
			headers.Add("Host", to)
			// This is a hack to remain infrastructure agnostic when running
			// We actually call the host set above not the endpoint we pass
			resp, err := from.Call(from.EndpointForPort(80), apps.AppCallOptions{
				Protocol: apps.AppProtocolHTTP,
				Headers:  headers,
			})
			for _, r := range resp {
				if match := serviceBHostname.FindString(r.Hostname); len(match) == 0 {
					t.Errorf("Request was made to service %v, all requests should go to the instance of b", r.Hostname)
				}
			}
			if err != nil {
				t.Errorf("Send to fake-external-service.com failed: %v", err)
			}
		}
	}
}

var fakeExternalServiceConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: fake-service-entry
spec:
  hosts:
  - fake-external-service.com
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: DNS
  location: MESH_EXTERNAL
  endpoints:
  - address: b
    locality: region/zone/subzone
  - address: c
    locality: notregion/notzone/notsubzone
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: fake-service-route
spec:
  hosts:
  - fake-external-service.com
  http:
  - route:
    - destination:
        host: fake-external-service.com
---
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: fake-service-destination
spec:
  host: fake-external-service.com
  trafficPolicy:
    outlierDetection:
      consecutiveErrors: 100
      interval: 1s
      baseEjectionTime: 3m
      maxEjectionPercent: 100
`
