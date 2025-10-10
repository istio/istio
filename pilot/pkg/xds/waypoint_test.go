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
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	upstream "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
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

func TestWaypointClusterWithDynamicDNS(t *testing.T) {
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
	cluster := clusters["inbound-vip|80|http|*.domain.com"]
	g.Expect(cluster.TransportSocket.GetTypedConfig().TypeUrl).
		To(Equal("type.googleapis.com/envoy.extensions.transport_sockets.internal_upstream.v3.InternalUpstreamTransport"))
	g.Expect(cluster).NotTo(BeNil())
	clusterType := cluster.ClusterDiscoveryType.(*clusterv3.Cluster_ClusterType)
	g.Expect(clusterType).NotTo(BeNil())
	g.Expect(clusterType.ClusterType.GetTypedConfig().TypeUrl).
		To(Equal("type.googleapis.com/envoy.extensions.clusters.dynamic_forward_proxy.v3.ClusterConfig"))
	po := cluster.TypedExtensionProtocolOptions["envoy.extensions.upstreams.http.v3.HttpProtocolOptions"]
	g.Expect(po).To(BeNil())
}

func TestWaypointClusterWithDynamicDNSWithoutWaypoint(t *testing.T) {
	g := NewWithT(t)
	dynamicDNSServiceEntry := `apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: wildcard-service-entry
  namespace: default
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
	var clusterNames []string
	for _, c := range clusters {
		clusterNames = append(clusterNames, c.Name)
	}
	g.Expect(len(clusterNames)).To(Equal(4))
	g.Expect(clusterNames).To(ContainElements([]string{
		"connect_originate",
		"outbound|15008||waypoint.default.svc.cluster.local",
		"main_internal",
		"encap"}))
}

func TestWaypointClusterWithDynamicDNSAndTLSOrigination(t *testing.T) {
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
    name: outbound-tls-1
    protocol: HTTP
  - number: 81
    name: outbound-tls-2
    protocol: HTTP
  resolution: DYNAMIC_DNS`
	// TODO(jaellio): Also set trust bundle
	destinationRule := `apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: domain-tls
  namespace: default
spec:
  host: "*.domain.com"
  trafficPolicy:
    tls:
      mode: SIMPLE`
	d, p := setupWaypointTest(t,
		waypointGateway,
		waypointSvc,
		waypointInstance,
		dynamicDNSServiceEntry,
		destinationRule)

	clusters := xdstest.ExtractClusters(d.Clusters(p))
	tlsCluster1 := clusters["inbound-vip|80|http|*.domain.com"]
	g.Expect(tlsCluster1).ToNot(BeNil())
	g.Expect(tlsCluster1.TransportSocket.GetTypedConfig().TypeUrl).
		To(Equal("type.googleapis.com/envoy.extensions.transport_sockets.internal_upstream.v3.InternalUpstreamTransport"))
	tlsCluster2 := clusters["inbound-vip|81|http|*.domain.com"]
	g.Expect(tlsCluster2).ToNot(BeNil())
	g.Expect(tlsCluster2.TransportSocket.GetTypedConfig().TypeUrl).
		To(Equal("type.googleapis.com/envoy.extensions.transport_sockets.internal_upstream.v3.InternalUpstreamTransport"))
	// Here we make sure we add validation of SAN for the upstream
	// connections generated by the DFP cluster. That's required by
	// DFP clusters when `allow_insecure_cluster_options` is false.
	po := tlsCluster1.TypedExtensionProtocolOptions["envoy.extensions.upstreams.http.v3.HttpProtocolOptions"]
	g.Expect(po).NotTo(BeNil())
	var httpOptions upstream.HttpProtocolOptions
	err := po.UnmarshalTo(&httpOptions)
	g.Expect(err).To(BeNil())

	g.Expect(httpOptions.UpstreamHttpProtocolOptions.AutoSni).To(BeTrue())
	g.Expect(httpOptions.UpstreamHttpProtocolOptions.AutoSanValidation).To(BeTrue())
}

func TestWaypointFiltersWithDynamicDns(t *testing.T) {
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
    name: http-1
    protocol: HTTP
  - number: 8080
    name: http-2
    protocol: HTTP
  resolution: DYNAMIC_DNS`
	d, p := setupWaypointTest(t,
		waypointGateway,
		waypointSvc,
		waypointInstance,
		dynamicDNSServiceEntry)

	listeners := make(map[string]*listenerv3.Listener)
	for _, l := range d.Listeners(p) {
		listeners[l.Name] = l
	}

	// Making sure the filter chains are there
	mainInternalListener := listeners["main_internal"]
	g.Expect(mainInternalListener).NotTo(BeNil())
	filterChainNames := xdstest.ExtractFilterChainNames(mainInternalListener)
	g.Expect(filterChainNames).To(ContainElements(
		"inbound-vip|80|http|*.domain.com",
		"inbound-vip|8080|http|*.domain.com",
	))

	// Making sure the virtual host matches the wildcarded domain
	// so malicious apps can't exploit the Host header.
	filterChain := xdstest.ExtractFilterChain("inbound-vip|80|http|*.domain.com", mainInternalListener)
	httpConnMngr := xdstest.ExtractHTTPConnectionManager(t, filterChain)
	g.Expect(httpConnMngr).NotTo(BeNil())
	routeConfig := httpConnMngr.GetRouteConfig()
	virtualHosts := xdstest.ExtractVirtualHosts(routeConfig)
	g.Expect(virtualHosts["*.domain.com"]).To(Equal([]string{
		"inbound-vip|80|http|*.domain.com",
	}))

	// Make sure we have the DFP filter in place
	var dfpFilter *hcm.HttpFilter
	for _, f := range httpConnMngr.HttpFilters {
		if f.Name == "envoy.filters.http.dynamic_forward_proxy" {
			dfpFilter = f
		}
	}
	g.Expect(dfpFilter).NotTo(BeNil())
	g.Expect(dfpFilter.GetTypedConfig().TypeUrl).To(Equal("type.googleapis.com/envoy.extensions.filters.http.dynamic_forward_proxy.v3.FilterConfig"))
}

func TestWaypointFiltersWithDynamicDnsAndHTTPRoutes(t *testing.T) {
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
	httpRoute := `apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: external-redirect-route
  namespace: default
spec:
  hostnames:
  - domain.com
  parentRefs:
  - group: networking.istio.io
    kind: ServiceEntry
    name: wildcard-service-entry
  rules:
  - filters:
    - requestRedirect:
        hostname: external.example.com
        path:
          replacePrefixMatch: /external
          type: ReplacePrefixMatch
        scheme: https
        statusCode: 301
      type: RequestRedirect
    matches:
    - path:
        type: PathPrefix
        value: /external
  - matches:
    - path:
        type: PathPrefix
        value: /other
    backendRefs:
      - kind: Hostname
        group: networking.istio.io
        name: "*.domain.org"
        port: 80`
	d, p := setupWaypointTest(t,
		waypointGateway,
		waypointSvc,
		waypointInstance,
		dynamicDNSServiceEntry,
		httpRoute)

	listeners := make(map[string]*listenerv3.Listener)
	for _, l := range d.Listeners(p) {
		listeners[l.Name] = l
	}

	mainInternalListener := listeners["main_internal"]
	g.Expect(mainInternalListener).NotTo(BeNil())
	filterChain := xdstest.ExtractFilterChain("inbound-vip|80|http|*.domain.com", mainInternalListener)
	httpConnMngr := xdstest.ExtractHTTPConnectionManager(t, filterChain)
	routeConfig := httpConnMngr.GetRouteConfig()
	virtualHosts := xdstest.ExtractVirtualHosts(routeConfig)

	g.Expect(virtualHosts).NotTo(BeNil())
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
