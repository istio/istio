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

package locality

import (
	"bytes"
	"net/http"
	"regexp"
	"sync"
	"testing"
	"time"

	"text/template"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/apps"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/resource"
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
//  CDS Test
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
//
//
//  EDS Test
//
//                                                       +-> 10.28.1.138 (B -> region.zone.subzone)
//                                                       |
//                                                  100% |
//                                                       |
// A (region.zone.subzone) -> fake-external-service.com -|
//                                                       |
//                                                    0% |
//                                                       |
//                                                       +-> 10.28.1.139 (C -> notregion.notzone.notsubzone)

const (
	testDuration = 1 * time.Minute
	numSendTasks = 16
)

var serviceBHostname = regexp.MustCompile("^b-.*$")

type ServiceConfig struct {
	Resolution      string
	ServiceBAddress string
	ServiceCAddress string
}

// WARNING: If these tests are failing with 404s it is likely that you will need to increase the Pilot propagation sleep!
func TestLocalityPrioritizedLoadBalancing(t *testing.T) {
	ctx := framework.NewContext(t)
	defer ctx.Done(t)

	ctx.RequireOrSkip(t, environment.Kube)

	// Share Istio Control Plane
	g := galley.NewOrFail(t, ctx, galley.Config{})
	p := pilot.NewOrFail(t, ctx, pilot.Config{Galley: g})

	t.Run("TestCDS", func(t *testing.T) {
		testLocalityPrioritizedLoadBalancingCDS(t, ctx, g, p)
	})

	t.Run("TestEDS", func(t *testing.T) {
		testLocalityPrioritizedLoadBalancingEDS(t, ctx, g, p)
	})

}

func testLocalityPrioritizedLoadBalancingCDS(t *testing.T, ctx resource.Context, g galley.Instance, p pilot.Instance) {
	t.Parallel()
	instance := apps.NewOrFail(ctx, t, apps.Config{Pilot: p})
	a := instance.GetAppOrFail("a", t).(apps.KubeApp)

	se := ServiceConfig{"DNS", "b", "c"}
	tmpl, _ := template.New("CDSServiceConfig").Parse(fakeExternalServiceConfig)
	var buf bytes.Buffer
	tmpl.Execute(&buf, se)
	g.ApplyConfigOrFail(t, instance.Namespace(), buf.String())

	// TODO: find a better way to do this!
	// This sleep allows config to propagate
	time.Sleep(30 * time.Second)

	// Send traffic to service B via a service entry for the test duration.
	wg := &sync.WaitGroup{}
	wg.Add(numSendTasks)
	log.Info("Sending traffic to local service (CDS)")
	for i := 0; i < numSendTasks; i++ {
		go sendTraffic(t, testDuration, a, "fake-external-service.com", wg)
	}
	wg.Wait()
}

func testLocalityPrioritizedLoadBalancingEDS(t *testing.T, ctx resource.Context, g galley.Instance, p pilot.Instance) {
	t.Parallel()
	instance := apps.NewOrFail(ctx, t, apps.Config{Pilot: p})
	a := instance.GetAppOrFail("a", t).(apps.KubeApp)
	b := instance.GetAppOrFail("b", t).(apps.KubeApp)
	c := instance.GetAppOrFail("c", t).(apps.KubeApp)

	se := ServiceConfig{
		"STATIC",
		b.EndpointForPort(80).NetworkEndpoint().Address,
		c.EndpointForPort(80).NetworkEndpoint().Address,
	}
	tmpl, _ := template.New("EDSServiceConfig").Parse(fakeExternalServiceConfig)
	var buf bytes.Buffer
	tmpl.Execute(&buf, se)
	g.ApplyConfigOrFail(t, instance.Namespace(), buf.String())

	// TODO: find a better way to do this!
	// This sleep allows config to propagate
	time.Sleep(20 * time.Second)

	// Send traffic to service B via a service entry for the test duration.
	wg := &sync.WaitGroup{}
	wg.Add(numSendTasks)
	log.Info("Sending traffic to local service (EDS)")
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
  resolution: {{.Resolution}}
  location: MESH_EXTERNAL
  endpoints:
  - address: {{.ServiceBAddress}}
    locality: region/zone/subzone
  - address: {{.ServiceCAddress}}
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
