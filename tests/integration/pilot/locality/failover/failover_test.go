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

package failover

import (
	"bytes"
	"fmt"
	"math/rand"
	"regexp"
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
	"istio.io/istio/tests/integration/pilot/locality"
)

// This test allows for Locality Load Balancing Failover testing without needing Kube nodes in multiple regions.
// We do this by overriding the calling service's locality with the istio-locality label and the serving
// service's locality with a service entry.
//
// Failover is set in the mesh config as follows:
// failover:
// - from: region
// 	 to: closeregion
//
// The created Service Entry for fake-(cds|eds)-external-service-12345.com points the domain at services
// that exist internally in the mesh. In the Service Entry we set a non-existent service or IP to be in the same
// locality as the caller (A). Service B to be in the primary failover locality and service C to be in the secondary failover locality.
// For CDS, the endpoint pool for the service in the local locality is empty so it fails over to service B.
// For EDS, when the request to the local locality fails outlier detection ejects the fake IP and fails over to service B.
// We then verify that all request go to service B. For details check the Service Entry configuration at the bottom of the page.
//
//  CDS Test
//
//                                                            100% +-> b (closeregion.zone.subzone)
//                                                                 |
//                                                                 |
//                                                                 |
// A (region.zone.subzone) -> fake-cds-external-service-12345.com -|-> c (notcloseregion.zone.subzone)
//                                                                 |
//                                                                 |
//                                                                 |
//                                                                 +-> NonExistentService (region.zone.subzone)
//
//
//  EDS Test
//
//                                                            100% +-> 10.28.1.138 (b -> closeregion.zone.subzone)
//                                                                 |
//                                                                 |
//                                                                 |
// A (region.zone.subzone) -> fake-eds-external-service-12345.com -|-> 10.28.1.139 (c -> notcloseregion.zone.subzone)
//                                                                 |
//                                                                 |
//                                                                 |
//                                                                 +-> 10.10.10.10 (NonExistentService -> region.zone.subzone)
//

const (
	testDuration = 10 * time.Second
)

var serviceBHostname = regexp.MustCompile("^b-.*$")

type ServiceConfig struct {
	Name               string
	Host               string
	Namespace          string
	Resolution         string
	ServiceBAddress    string
	ServiceCAddress    string
	NonExistantService string
}

func TestLocalityFailover(t *testing.T) {
	ctx := framework.NewContext(t)
	defer ctx.Done(t)

	ctx.RequireOrSkip(t, environment.Kube)

	// Share Istio Control Plane
	g := galley.NewOrFail(t, ctx, galley.Config{})
	p := pilot.NewOrFail(t, ctx, pilot.Config{Galley: g})

	rand.Seed(time.Now().UnixNano())
	framework.Run(t, func(ctx framework.TestContext) {
		testCDS(t, ctx, g, p)
	})

	framework.Run(t, func(ctx framework.TestContext) {
		testEDS(t, ctx, g, p)
	})
}

func testCDS(t *testing.T, ctx resource.Context, g galley.Instance, p pilot.Instance) {
	instance := apps.NewOrFail(t, ctx, apps.Config{Pilot: p, Galley: g})
	a := instance.GetAppOrFail("a", t).(apps.KubeApp)

	fakeHostname := fmt.Sprintf("fake-cds-external-service-%v.com", rand.Int())
	se := ServiceConfig{
		"lplb-failover-cds-service-entry",
		fakeHostname,
		instance.Namespace().Name(),
		"DNS",
		"b",
		"c",
		"nonexistantservice",
	}

	tmpl, _ := template.New("CDSServiceConfig").Parse(fakeExternalServiceConfig)
	var buf bytes.Buffer
	tmpl.Execute(&buf, se)
	g.ApplyConfigOrFail(t, instance.Namespace(), buf.String())

	// TODO: find a better way to do this!
	// This sleep allows config to propagate
	time.Sleep(10 * time.Second)

	// Send traffic to service B via a service entry for the test duration.
	log.Infof("Sending traffic to local service (CDS) via %v", fakeHostname)
	locality.SendTraffic(t, testDuration, a, fakeHostname, serviceBHostname)
}

func testEDS(t *testing.T, ctx resource.Context, g galley.Instance, p pilot.Instance) {
	instance := apps.NewOrFail(t, ctx, apps.Config{Pilot: p, Galley: g})
	a := instance.GetAppOrFail("a", t).(apps.KubeApp)
	b := instance.GetAppOrFail("b", t).(apps.KubeApp)
	c := instance.GetAppOrFail("c", t).(apps.KubeApp)

	fakeHostname := fmt.Sprintf("fake-eds-external-service-%v.com", rand.Int())
	se := ServiceConfig{
		"lplb-failover-eds-service-entry",
		fakeHostname,
		instance.Namespace().Name(),
		"STATIC",
		b.EndpointForPort(80).NetworkEndpoint().Address,
		c.EndpointForPort(80).NetworkEndpoint().Address,
		"10.10.10.10",
	}

	tmpl, _ := template.New("EDSServiceConfig").Parse(fakeExternalServiceConfig)
	var buf bytes.Buffer
	tmpl.Execute(&buf, se)
	g.ApplyConfigOrFail(t, instance.Namespace(), buf.String())

	// TODO: find a better way to do this!
	// This sleep allows config to propagate
	time.Sleep(10 * time.Second)

	// Send traffic to service B via a service entry for the test duration.
	log.Infof("Sending traffic to local service (EDS) via %v", fakeHostname)
	locality.SendTraffic(t, testDuration, a, fakeHostname, serviceBHostname)
}

var fakeExternalServiceConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
spec:
  hosts:
  - {{.Host}}
  exportTo:
  - "."
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: {{.Resolution}}
  location: MESH_EXTERNAL
  endpoints:
  - address: {{.NonExistantService}}
    locality: region/zone/subzone
  - address: {{.ServiceBAddress}}
    locality: closeregion/zone/subzone
  - address: {{.ServiceCAddress}}
    locality: notcloseregion/zone/subzone
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: fake-service-route
  namespace: {{.Namespace}}
spec:
  hosts:
  - {{.Host}}
  http:
  - route:
    - destination:
        host: {{.Host}}
    retries:
      attempts: 3
      perTryTimeout: 1s
      retryOn: gateway-error,connect-failure,refused-stream
---
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: fake-service-destination
  namespace: {{.Namespace}}
spec:
  host: {{.Host}}
  trafficPolicy:
    outlierDetection:
      consecutiveErrors: 1
      interval: 1s
      baseEjectionTime: 3m
      maxEjectionPercent: 100
`
