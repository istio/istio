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

	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

const (
	// Istio Gateway as produced by the Gateway API conversion of an istio-east-west Gateway with a
	// TLS passthrough listener on port 15443 (see testdata/east-west-ambient.yaml).
	eastWestAutoPassthroughGateway = `apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: eastwestgateway
  namespace: istio-system
spec:
  selector:
    istio: eastwestgateway
  servers:
  - port:
      number: 15443
      name: cross-network
      protocol: TLS
    hosts:
    - "*"
    tls:
      mode: AUTO_PASSTHROUGH
`
	eastWestAppServiceEntry = `apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: app
  namespace: default
spec:
  hosts: [app.com]
  addresses: [1.2.3.4]
  location: MESH_INTERNAL
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: STATIC
  workloadSelector:
    labels:
      app: app
`
	// A sidecar workload: it advertises Istio mTLS, so AUTO_PASSTHROUGH may forward to it.
	eastWestSidecarWorkloadEntry = `apiVersion: networking.istio.io/v1
kind: WorkloadEntry
metadata:
  name: app-sidecar
  namespace: default
  labels:
    app: app
    security.istio.io/tlsMode: istio
spec:
  address: 1.1.1.1
  network: network-1
`
	// An ambient workload: it only speaks HBONE, so it must not receive passthrough mTLS traffic.
	eastWestAmbientWorkloadEntry = `apiVersion: networking.istio.io/v1
kind: WorkloadEntry
metadata:
  name: app-ambient
  namespace: default
  labels:
    app: app
  annotations:
    ambient.istio.io/redirection: enabled
spec:
  address: 1.1.1.2
  network: network-1
`
)

func setupAmbientEastWestGatewayTest(t *testing.T, configs ...string) (*xds.FakeDiscoveryServer, *model.Proxy) {
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
	d := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: joinYaml(configs...),
	})
	proxy := d.SetupProxy(&model.Proxy{
		Type:            model.Waypoint,
		ConfigNamespace: "istio-system",
		IPAddresses:     []string{"10.0.0.1"},
		Labels: map[string]string{
			"istio":                   "eastwestgateway",
			label.GatewayManaged.Name: constants.ManagedGatewayEastWestControllerLabel,
		},
		Metadata: &model.NodeMetadata{
			Network: "network-1",
		},
	})
	return d, proxy
}

// TestAmbientEastWestGatewayAutoPassthrough verifies that a single ambient east-west gateway can serve
// both HBONE (ambient) and AUTO_PASSTHROUGH (sidecar multi-network) traffic.
func TestAmbientEastWestGatewayAutoPassthrough(t *testing.T) {
	sniDnatCluster := model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, "", "app.com", 80)

	t.Run("with auto passthrough server", func(t *testing.T) {
		d, proxy := setupAmbientEastWestGatewayTest(t,
			eastWestAutoPassthroughGateway, eastWestAppServiceEntry, eastWestSidecarWorkloadEntry, eastWestAmbientWorkloadEntry)

		if proxy.MergedGateway == nil || !proxy.MergedGateway.ContainsAutoPassthroughGateways {
			t.Fatalf("expected the east-west gateway to merge an AUTO_PASSTHROUGH server, got %+v", proxy.MergedGateway)
		}

		// LDS: the HBONE path is untouched, and an AUTO_PASSTHROUGH listener routes by SNI-DNAT cluster name.
		listeners := d.Listeners(proxy)
		names := sets.New(xdstest.ExtractListenerNames(listeners)...)
		for _, expected := range []string{"connect_terminate", "main_internal", "0.0.0.0_15443"} {
			assert.Equal(t, names.Contains(expected), true, "missing listener "+expected)
		}
		l := xdstest.ExtractListener("0.0.0.0_15443", listeners)
		assert.Equal(t, l.GetAddress().GetSocketAddress().GetPortValue(), uint32(15443))
		_, hasTLSInspector := xdstest.ExtractListenerFilters(l)["envoy.filters.listener.tls_inspector"]
		assert.Equal(t, hasTLSInspector, true, "auto passthrough listener needs the TLS inspector for SNI routing")
		fc := slices.FindFunc(l.FilterChains, func(fc *listenerv3.FilterChain) bool {
			return slices.Equal(fc.GetFilterChainMatch().GetServerNames(), []string{sniDnatCluster})
		})
		if fc == nil {
			t.Fatalf("no filter chain matching SNI %q, got %v", sniDnatCluster, xdstest.ExtractFilterChainNames(l))
		}
		assert.Equal(t, sets.New((*fc).GetFilterChainMatch().GetApplicationProtocols()...).Contains("istio"), true,
			"auto passthrough filter chain must match Istio mTLS ALPNs")
		assert.Equal(t, xdstest.ExtractTCPProxy(t, *fc).GetCluster(), sniDnatCluster)

		// CDS: the SNI-DNAT cluster exists, like on a classic east-west gateway.
		clusters := d.Clusters(proxy)
		c := xdstest.ExtractCluster(sniDnatCluster, clusters)
		if c == nil {
			t.Fatalf("missing SNI-DNAT cluster %q, got %v", sniDnatCluster, xdstest.ExtractEdsClusterNames(clusters))
		}

		// EDS: only workloads that accept Istio mTLS are reachable through passthrough; ambient workloads
		// (HBONE only) are filtered out, exactly like on a classic east-west gateway.
		eps := xdstest.ExtractLoadAssignments(d.Endpoints(proxy))
		assert.Equal(t, eps[sniDnatCluster], []string{"1.1.1.1:80"})
	})

	t.Run("without auto passthrough server", func(t *testing.T) {
		d, proxy := setupAmbientEastWestGatewayTest(t,
			eastWestAppServiceEntry, eastWestSidecarWorkloadEntry, eastWestAmbientWorkloadEntry)

		listeners := d.Listeners(proxy)
		names := sets.New(xdstest.ExtractListenerNames(listeners)...)
		assert.Equal(t, names.Contains("connect_terminate"), true)
		assert.Equal(t, names.Contains("0.0.0.0_15443"), false)
		assert.Equal(t, xdstest.ExtractCluster(sniDnatCluster, d.Clusters(proxy)) == nil, true,
			"SNI-DNAT clusters must only be built for AUTO_PASSTHROUGH gateways")
	})
}

// TestAmbientEastWestGatewayPassthroughAuthorization checks that the TCP, TLS passthrough and ISTIO_MUTUAL listeners
// of an ambient east-west gateway, which are real listeners although the proxy is a waypoint, read the downstream peer from the
// connection: ipBlocks match the direct remote address and principals the TLS peer. See
// https://github.com/istio/istio/issues/62522.
func TestAmbientEastWestGatewayPassthroughAuthorization(t *testing.T) {
	gw := `apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: eastwestgateway
  namespace: istio-system
spec:
  selector:
    istio: eastwestgateway
  servers:
  - port:
      number: 9000
      name: tcp
      protocol: TCP
    hosts:
    - "*"
  - port:
      number: 8443
      name: tls
      protocol: TLS
    hosts:
    - app.com
    tls:
      mode: PASSTHROUGH
  - port:
      number: 9443
      name: tls-istio-mutual
      protocol: TLS
    hosts:
    - "*"
    tls:
      mode: ISTIO_MUTUAL
`
	vs := `apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: app
  namespace: istio-system
spec:
  hosts:
  - app.com
  gateways:
  - istio-system/eastwestgateway
  tcp:
  - match:
    - port: 9000
    route:
    - destination:
        host: app.com
        port:
          number: 80
  - match:
    - port: 9443
    route:
    - destination:
        host: app.com
        port:
          number: 80
  tls:
  - match:
    - port: 8443
      sniHosts:
      - app.com
    route:
    - destination:
        host: app.com
        port:
          number: 80
`
	policy := `apiVersion: security.istio.io/v1
kind: AuthorizationPolicy
metadata:
  name: source-ip
  namespace: istio-system
spec:
  selector:
    matchLabels:
      istio: eastwestgateway
  action: DENY
` + sourceIPPolicyRules + `  - from:
    - source:
        principals: ["cluster.local/ns/default/sa/client"]
`
	d, proxy := setupAmbientEastWestGatewayTest(t, gw, vs, policy, eastWestAppServiceEntry, eastWestSidecarWorkloadEntry)
	listeners := d.Listeners(proxy)

	// 9443 terminates ISTIO_MUTUAL on the gateway, so principals can match its client certificate.
	for _, name := range []string{"0.0.0.0_9000", "0.0.0.0_8443", "0.0.0.0_9443"} {
		l := xdstest.ExtractListener(name, listeners)
		assertSourceIPMatching(t, l, false)
		for _, cfg := range rbacConfigs(t, l) {
			if !strings.Contains(cfg, "source-ip") {
				continue
			}
			if strings.Contains(cfg, `"io.istio.peer_principal"`) || !strings.Contains(cfg, `"authenticated"`) {
				t.Fatalf("%s: principals must match the TLS peer of the connection:\n%s", name, cfg)
			}
		}
	}
}
