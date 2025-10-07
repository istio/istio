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

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	dfpcluster "github.com/envoyproxy/go-control-plane/envoy/extensions/clusters/dynamic_forward_proxy/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	. "github.com/onsi/gomega"

	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
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
    istio.io/ingress-use-waypoint: "true"
spec:
  hosts: [app.com]
  addresses: [1.2.3.4]
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

func TestWaypointNetworkFilters(t *testing.T) {
	// Define two SE with various protocol declarations, we will check how sniffing and TLS inspection are enabled.

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
  - number: 443
    name: https
    protocol: TLS
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
  - number: 443
    name: https
    protocol: TCP
`

	d, proxy := setupWaypointTest(t,
		waypointGateway,
		waypointSvc,
		waypointInstance,
		appServiceEntry, app2ServiceEntry)

	l := xdstest.ExtractListener("main_internal", d.Listeners(proxy))
	filters := xdstest.ExtractListenerFilters(l)

	httpInspectorDisabled := filters[wellknown.HTTPInspector].GetFilterDisabled()
	hasSniffing := func(port int, expect bool) {
		t.Helper()
		assert.Equal(t, xdstest.EvaluateListenerFilterPredicates(httpInspectorDisabled, port), !expect)
	}
	hasSniffing(80, true)   // HTTP and TCP on same port
	hasSniffing(81, false)  // HTTP
	hasSniffing(91, false)  // TCP
	hasSniffing(90, true)   // Unspecified
	hasSniffing(443, false) // TLS and TCP on the same port - HTTP inspector not needed

	tlsInspectorDisabled := filters[wellknown.TLSInspector].GetFilterDisabled()
	hasTLSInspector := func(port int, expect bool) {
		t.Helper()
		assert.Equal(t, xdstest.EvaluateListenerFilterPredicates(tlsInspectorDisabled, port), !expect)
	}
	hasTLSInspector(80, false)
	hasTLSInspector(81, false)
	hasTLSInspector(91, false)
	hasTLSInspector(90, false)
	hasTLSInspector(443, true)
}

func TestWaypointEndpoints(t *testing.T) {
	d, proxy := setupWaypointTest(t,
		waypointGateway,
		waypointSvc,
		appPod, appPodNoMesh,
		waypointInstance, appWorkloadEntry,
		appServiceEntry)

	eps := slices.Sort(xdstest.ExtractEndpoints(d.Endpoints(proxy)[1]))
	assert.Equal(t, eps, []string{
		// No tunnel, should get dual IPs
		"1.1.1.3:80,[2001:20::3]:80",
		"connect_originate;1.1.1.1:80",
		// Tunnel doesn't support multiple IPs
		"connect_originate;1.1.1.2:80",
	})
}

func TestWaypointTelemetry(t *testing.T) {
	telemetry := `apiVersion: telemetry.istio.io/v1
kind: Telemetry
metadata:
  name: logs
  namespace: default
spec:
  targetRefs:
  - kind: ServiceEntry
    name: app
    group: networking.istio.io
  accessLogging:
  - providers:
    - name: envoy
  metrics:
  - providers:
    - name: prometheus
  tracing:
  - providers:
    - name: otel
`
	notSelectedAppServiceEntry := `apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: not-app
  namespace: default
  labels:
    istio.io/use-waypoint: waypoint
spec:
  hosts: [not-app.com]
  addresses: [2.3.4.5]
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: STATIC
  workloadSelector:
    labels:
      app: app`
	d, proxy := setupWaypointTest(t,
		waypointGateway, waypointSvc, waypointInstance,
		appServiceEntry, notSelectedAppServiceEntry,
		telemetry)

	l := xdstest.ExtractListener("main_internal", d.Listeners(proxy))
	app := xdstest.ExtractHTTPConnectionManager(t,
		xdstest.ExtractFilterChain(model.BuildSubsetKey(model.TrafficDirectionInboundVIP, "http", "app.com", 80), l))
	notApp := xdstest.ExtractHTTPConnectionManager(t,
		xdstest.ExtractFilterChain(model.BuildSubsetKey(model.TrafficDirectionInboundVIP, "http", "not-app.com", 80), l))

	// The selected service should get all 3 types
	assert.Equal(t, app.AccessLog != nil, true)
	assert.Equal(t, sets.New(slices.Map(app.HttpFilters, (*hcm.HttpFilter).GetName)...).Contains("istio.stats"), true)
	assert.Equal(t, app.Tracing.Provider != nil, true)

	// Unselected service should get none
	assert.Equal(t, notApp.AccessLog == nil, true)
	assert.Equal(t, sets.New(slices.Map(notApp.HttpFilters, (*hcm.HttpFilter).GetName)...).Contains("istio.stats"), false)
	assert.Equal(t, notApp.Tracing.Provider == nil, true)
}

func TestWaypointRequestAuth(t *testing.T) {
	// jwks mostly from https://datatracker.ietf.org/doc/html/rfc7517#appendix-A.1
	requestAuthn := `apiVersion: security.istio.io/v1
kind: RequestAuthentication
metadata:
  name: jwt-on-waypoint
  namespace: default
spec:
  targetRefs:
  - group: networking.istio.io
    kind: ServiceEntry
    name: app
  jwtRules:
  - issuer: "example.ietf.org"
    jwks: |
          {"keys":
            [
              {"kty":"EC",
              "crv":"P-256",
              "x":"MKBCTNIcKUSDii11ySs3526iDZ8AiTo7Tu6KPAqv7D4",
              "y":"4Etl6SRW2YiLUrN5vfvVHuhp7x8PxltmWWlbbM4IFyM",
              "use":"enc",
              "kid":"1"}
            ]
          }`
	notSelectedAppServiceEntry := `apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: not-app
  namespace: default
  labels:
    istio.io/use-waypoint: waypoint
spec:
  hosts: [not-app.com]
  addresses: [2.3.4.5]
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: STATIC
  workloadSelector:
    labels:
      app: app`
	d, proxy := setupWaypointTest(t,
		waypointGateway, waypointSvc, waypointInstance,
		appServiceEntry, notSelectedAppServiceEntry, requestAuthn)

	l := xdstest.ExtractListener("main_internal", d.Listeners(proxy))

	app := xdstest.ExtractHTTPConnectionManager(t,
		xdstest.ExtractFilterChain(model.BuildSubsetKey(model.TrafficDirectionInboundVIP, "http", "app.com", 80), l))
	assert.Equal(t, sets.New(slices.Map(app.HttpFilters, (*hcm.HttpFilter).GetName)...).Contains("envoy.filters.http.jwt_authn"), true)

	// assert not-app.com has not gotten config from the req authn
	notApp := xdstest.ExtractHTTPConnectionManager(t,
		xdstest.ExtractFilterChain(model.BuildSubsetKey(model.TrafficDirectionInboundVIP, "http", "not-app.com", 80), l))
	assert.Equal(t, sets.New(slices.Map(notApp.HttpFilters, (*hcm.HttpFilter).GetName)...).Contains("envoy.filters.http.jwt_authn"), false)
}

func TestCrossNamespaceWaypointRequestAuth(t *testing.T) {
	// jwks mostly from https://datatracker.ietf.org/doc/html/rfc7517#appendix-A.1
	requestAuthn := `apiVersion: security.istio.io/v1
kind: RequestAuthentication
metadata:
  name: jwt-on-waypoint
  namespace: app
spec:
  targetRefs:
  - group: networking.istio.io
    kind: ServiceEntry
    name: app
  jwtRules:
  - issuer: "example.ietf.org"
    jwks: |
          {"keys":
            [
              {"kty":"EC",
              "crv":"P-256",
              "x":"MKBCTNIcKUSDii11ySs3526iDZ8AiTo7Tu6KPAqv7D4",
              "y":"4Etl6SRW2YiLUrN5vfvVHuhp7x8PxltmWWlbbM4IFyM",
              "use":"enc",
              "kid":"1"}
            ]
          }`
	waypointSvc := `apiVersion: v1
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
    gateway.networking.k8s.io/gateway-name: waypoint`

	waypointInstance := `apiVersion: networking.istio.io/v1
kind: WorkloadEntry
metadata:
  name: waypoint-a
  namespace: default
spec:
  address: 3.0.0.1
  labels:
    gateway.networking.k8s.io/gateway-name: waypoint`

	waypointGateway := `apiVersion: gateway.networking.k8s.io/v1beta1
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
      allowedRoutes:
        namespaces:
          from: All
status:
  addresses:
  - type: Hostname
    value: waypoint.default.svc.cluster.local`

	appServiceEntry := `apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: app
  namespace: app 
  labels:
    istio.io/use-waypoint: waypoint
    istio.io/use-waypoint-namespace: default
spec:
  hosts: [app.com]
  addresses: [1.2.3.4]
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: STATIC
  workloadSelector:
    labels:
      app: app`
	notSelectedAppServiceEntry := `apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: not-app
  namespace: app
  labels:
    istio.io/use-waypoint: waypoint
    istio.io/use-waypoint-namespace: default
spec:
  hosts: [not-app.com]
  addresses: [2.3.4.5]
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: STATIC
  workloadSelector:
    labels:
      app: app`

	d, proxy := setupWaypointTest(t,
		waypointGateway, waypointSvc, waypointInstance,
		appServiceEntry, notSelectedAppServiceEntry, requestAuthn)

	l := xdstest.ExtractListener("main_internal", d.Listeners(proxy))

	app := xdstest.ExtractHTTPConnectionManager(t,
		xdstest.ExtractFilterChain(model.BuildSubsetKey(model.TrafficDirectionInboundVIP, "http", "app.com", 80), l))
	assert.Equal(t, sets.New(slices.Map(app.HttpFilters, (*hcm.HttpFilter).GetName)...).Contains("envoy.filters.http.jwt_authn"), true)

	// assert not-app.com has not gotten config from the req authn
	notApp := xdstest.ExtractHTTPConnectionManager(t,
		xdstest.ExtractFilterChain(model.BuildSubsetKey(model.TrafficDirectionInboundVIP, "http", "not-app.com", 80), l))
	assert.Equal(t, sets.New(slices.Map(notApp.HttpFilters, (*hcm.HttpFilter).GetName)...).Contains("envoy.filters.http.jwt_authn"), false)
}

func TestIngressUseWaypoint(t *testing.T) {
	d, _ := setupWaypointTest(t,
		waypointGateway,
		waypointSvc,
		appPod, appPodNoMesh,
		waypointInstance, appWorkloadEntry,
		appServiceEntry)
	ingressProxy := d.SetupProxy(&model.Proxy{
		Type:            model.Router,
		ConfigNamespace: "default",
		IPAddresses:     []string{"3.0.0.3"}, // Arbitrary
	})

	eps := slices.Sort(xdstest.ExtractLoadAssignments(d.Endpoints(ingressProxy))["outbound|80||app.com"])
	assert.Equal(t, eps, []string{
		// 1.2.3.4: Service VIP
		// 3.0.0.1: Waypoint Pod IP
		"connect_originate;1.2.3.4:80;3.0.0.1:15008",
	})
}

func TestWaypointWithDynamicDNS(t *testing.T) {
	g := NewWithT(t)
	dynamicDNSServiceEntry := `apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: wildcard-service-entry
  namespace: default
  labels:
    istio.io/use-waypoint: waypoint
    istio.io/use-waypoint-namespace: default
spec:
  hosts: ["*.domain.com"]
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: DYNAMIC_DNS`
	d, p := setupWaypointTest(t,
		waypointGateway,
		waypointSvc,
		waypointInstance,
		dynamicDNSServiceEntry)

	clusters := xdstest.ExtractClusters(d.Clusters(p))
	g.Expect(xdstest.MapKeys(clusters)).To(Equal([]string{
		"connect_originate",
		"encap",
		"inbound-vip|80|http|*.domain.com",
		"main_internal",
		"outbound|15008||waypoint.default.svc.cluster.local",
	}))
	cluster := clusters["inbound-vip|80|http|*.domain.com"]
	clusterType := cluster.ClusterDiscoveryType.(*clusterv3.Cluster_ClusterType)
	g.Expect(clusterType).NotTo(BeNil())
	dfpClusterConfig := &dfpcluster.ClusterConfig{}
	err := clusterType.ClusterType.TypedConfig.UnmarshalTo(dfpClusterConfig)
	g.Expect(err).To(BeNil())
}

func setupWaypointTest(t *testing.T, configs ...string) (*xds.FakeDiscoveryServer, *model.Proxy) {
	test.SetForTest(t, &features.EnableDualStack, true)
	test.SetForTest(t, &features.EnableIngressWaypointRouting, true)
	c := joinYaml(configs...)
	mc := mesh.DefaultMeshConfig()
	mc.ExtensionProviders = append(mc.ExtensionProviders, &meshconfig.MeshConfig_ExtensionProvider{
		Name: "otel",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_Opentelemetry{
			// Pointing to the waypoint is silly, we just need some valid service to point to and it exists
			Opentelemetry: &meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider{Service: "waypoint.default.svc.cluster.local", Port: 15008},
		},
	})
	// Ambient controller needs objects as kube, so apply to both
	d := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString:           c,
		KubernetesObjectString: c,
		MeshConfig:             mc,
	})
	proxy := d.SetupProxy(&model.Proxy{
		Type:            model.Waypoint,
		ConfigNamespace: "default",
		Labels:          map[string]string{label.IoK8sNetworkingGatewayGatewayName.Name: "waypoint"},
		IPAddresses:     []string{"3.0.0.1"}, // match the WE
	})
	return d, proxy
}

func joinYaml(s ...string) string {
	return strings.Join(s, "\n---\n")
}
