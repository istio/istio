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

package xds_test

import (
	"strings"
	"testing"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/wellknown"
)

var (
	waypointSvc = `apiVersion: v1
kind: Service
metadata:
  labels:
    gateway.istio.io/managed: istio.io-mesh-controller
    gateway.networking.k8s.io/gateway-name: waypoint
    istio.io/gateway-name: waypoint
  name: waypoint
  namespace: default
spec:
  clusterIP: 3.0.0.0
  ports:
  - appProtocol: hbone
    name: mesh
    port: 15008
  selector:
    gateway.networking.k8s.io/gateway-name: waypoint
`
	waypointInstance = `apiVersion: networking.istio.io/v1
kind: WorkloadEntry
metadata:
  name: waypoint-a
  namespace: default
spec:
  address: 3.0.0.1
  labels:
    gateway.networking.k8s.io/gateway-name: waypoint
`
	waypointGateway = `apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  name: waypoint
  namespace: default
spec:
  gatewayClassName: waypoint
  listeners:
    - name: mesh
      port: 15008
      protocol: HBONE
status:
  addresses:
  - type: Hostname
    value: waypoint.default.svc.cluster.local
`
	appServiceEntry = `apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: app
  namespace: default
  labels:
    istio.io/use-waypoint: waypoint
spec:
  hosts: [app.com]
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: STATIC
  workloadSelector:
    labels:
      app: app
`
	appWorkloadEntry = `apiVersion: networking.istio.io/v1
kind: WorkloadEntry
metadata:
  name: app-a
  namespace: default
  labels:
    app: app
  annotations:
    ambient.istio.io/redirection: enabled
spec:
  address: 1.1.1.1
`
	appPod = `apiVersion: v1
kind: Pod
metadata:
  name: app-b
  namespace: default
  labels:
    app: app
  annotations:
    ambient.istio.io/redirection: enabled
spec: {}
status:
  conditions:
  - status: "True"
    type: Ready
  podIP: 1.1.1.2
  podIPs:
  - ip: 1.1.1.2
  - ip: 2001:20::2
`
	appPodNoMesh = `apiVersion: v1
kind: Pod
metadata:
  name: app-c
  namespace: default
  labels:
    app: app
spec: {}
status:
  conditions:
  - status: "True"
    type: Ready
  podIP: 1.1.1.3
  podIPs:
  - ip: 1.1.1.3
  - ip: 2001:20::3
`
)

func TestWaypointSniffing(t *testing.T) {
	// Define two SE with various protocol declarations, we will check how sniffing is enabled.

	appServiceEntry := `apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: app
  namespace: default
  labels:
    istio.io/use-waypoint: waypoint
spec:
  hosts: [app.com]
  ports:
  - number: 80
    name: http # HTTP, but will have another TCP port overlapping in another service
    protocol: HTTP
  - number: 81
    name: http-only # HTTP
    protocol: HTTP
  - number: 90 # Sniffed, on its own
    name: auto
    protocol: ""
`
	app2ServiceEntry := `apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: app2
  namespace: default
  labels:
    istio.io/use-waypoint: waypoint
spec:
  hosts: [app2.com]
  ports:
  - number: 80
    name: tcp # conflicts with other HTTP port
    protocol: TCP
  - number: 91
    name: tcp-only # TCP
    protocol: HTTP
`
	d, proxy := setupWaypointTest(t,
		waypointGateway,
		waypointSvc,
		waypointInstance,
		appServiceEntry, app2ServiceEntry)

	l := xdstest.ExtractListener("main_internal", d.Listeners(proxy))
	filters := xdstest.ExtractListenerFilters(l)
	fd := filters[wellknown.HTTPInspector].GetFilterDisabled()
	hasSniffing := func(port int, expect bool) {
		t.Helper()
		assert.Equal(t, xdstest.EvaluateListenerFilterPredicates(fd, port), !expect)
	}
	hasSniffing(80, true)  // HTTP and TCP on same port
	hasSniffing(81, false) // HTTP
	hasSniffing(91, false) // TCP
	hasSniffing(90, true)  // Unspecified
}

func TestWaypoint(t *testing.T) {
	d, proxy := setupWaypointTest(t,
		waypointGateway,
		waypointSvc,
		appPod, appPodNoMesh,
		waypointInstance, appWorkloadEntry,
		appServiceEntry)

	eps := slices.Sort(xdstest.ExtractEndpoints(d.Endpoints(proxy)[0]))
	assert.Equal(t, eps, []string{
		// No tunnel, should get dual IPs
		"1.1.1.3:80,[2001:20::3]:80",
		"connect_originate;1.1.1.1:80",
		// Tunnel doesn't support multiple IPs
		"connect_originate;1.1.1.2:80",
	})
}

func TestWaypointTLSInspector(t *testing.T) {
	serviceHTTP := `apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: app
  namespace: default
  labels:
    istio.io/use-waypoint: waypoint
spec:
  hosts: [app.com]
  ports:
  - number: 80
    name: http
    protocol: HTTP
`
	serviceEntryHTTPandTLS := `apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: app
  namespace: default
  labels:
    istio.io/use-waypoint: waypoint
spec:
  hosts: [app.com]
  ports:
  - number: 80
    name: http
    protocol: HTTP
  - number: 443
    name: https
    protocol: HTTPS
  - number: 6443
    name: tls
    protocol: TLS
`
	testCases := []struct {
		name                   string
		service                string
		tlsInspectorUnexpected bool
	}{
		{
			name:                   "HTTP only",
			service:                serviceHTTP,
			tlsInspectorUnexpected: true,
		},
		{
			name:    "HTTP, HTTPS and TLS",
			service: serviceEntryHTTPandTLS,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			d, proxy := setupWaypointTest(t, waypointGateway, waypointSvc, waypointInstance, tc.service)
			l := xdstest.ExtractListener("main_internal", d.Listeners(proxy))
			filters := xdstest.ExtractListenerFilters(l)
			f, found := filters[wellknown.TLSInspector]

			if tc.tlsInspectorUnexpected {
				if found {
					t.Fatalf("Found unexpected TLS inspector")
				}
				return
			}

			hasTLSInspector := func(port int, expect bool) {
				t.Helper()
				assert.Equal(t, xdstest.EvaluateListenerFilterPredicates(f.GetFilterDisabled(), port), !expect)
			}
			hasTLSInspector(80, false)
			hasTLSInspector(443, true)
			hasTLSInspector(6443, true)
		})
	}
}

func setupWaypointTest(t *testing.T, configs ...string) (*xds.FakeDiscoveryServer, *model.Proxy) {
	test.SetForTest(t, &features.EnableAmbient, true)
	test.SetForTest(t, &features.EnableDualStack, true)
	c := joinYaml(configs...)
	// Ambient controller needs objects as kube, so apply to both
	d := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString:           c,
		KubernetesObjectString: c,
	})
	proxy := d.SetupProxy(&model.Proxy{
		Type:            model.Waypoint,
		ConfigNamespace: "default",
		IPAddresses:     []string{"3.0.0.1"}, // match the WE
	})
	return d, proxy
}

func joinYaml(s ...string) string {
	return strings.Join(s, "\n---\n")
}
