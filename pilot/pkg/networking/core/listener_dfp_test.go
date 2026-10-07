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

package core

import (
	"fmt"
	"strings"
	"testing"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	dfp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/dynamic_forward_proxy/v3"
	rbacnetwork "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/rbac/v3"
	sfsnetwork "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/set_filter_state/v3"
	snidfp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/sni_dynamic_forward_proxy/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	. "github.com/onsi/gomega"

	networking "istio.io/api/networking/v1alpha3"
	security "istio.io/api/security/v1beta1"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/wellknown"
)

// TestSidecarDynamicDNSFilters tests DFP filter creation for sidecar proxies
func TestSidecarDynamicDNSFilters(t *testing.T) {
	cases := []struct {
		name              string
		protocol          string
		port              uint32
		wildcardHost      bool
		expectHTTPFilter  bool
		expectSNIFilter   bool
		validateFilterCfg bool
	}{
		{
			name:              "HTTP protocol with wildcard host",
			protocol:          "HTTP",
			port:              80,
			wildcardHost:      true,
			expectHTTPFilter:  true,
			validateFilterCfg: true,
		},
		{
			name:              "TLS protocol with wildcard host",
			protocol:          "TLS",
			port:              443,
			wildcardHost:      true,
			expectSNIFilter:   true,
			validateFilterCfg: true,
		},
		{
			name:             "HTTP with non-wildcard host",
			protocol:         "HTTP",
			port:             80,
			wildcardHost:     false,
			expectHTTPFilter: false,
			expectSNIFilter:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			hostname := "example.com"
			if tc.wildcardHost {
				hostname = "*.svc.cluster.local"
			}

			serviceEntry := config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.ServiceEntry,
					Name:             "test-se",
					Namespace:        "default",
				},
				Spec: &networking.ServiceEntry{
					Hosts: []string{hostname},
					Ports: []*networking.ServicePort{
						{Number: tc.port, Name: "test-port", Protocol: tc.protocol},
					},
					Location:   networking.ServiceEntry_MESH_EXTERNAL,
					Resolution: networking.ServiceEntry_DYNAMIC_DNS,
				},
			}

			// For non-wildcard hosts, add endpoints
			if !tc.wildcardHost {
				serviceEntry.Spec.(*networking.ServiceEntry).Resolution = networking.ServiceEntry_STATIC
				serviceEntry.Spec.(*networking.ServiceEntry).Endpoints = []*networking.WorkloadEntry{
					{Address: "1.2.3.4"},
				}
			}

			cg := NewConfigGenTest(t, TestOptions{Configs: []config.Config{serviceEntry}})
			proxy := cg.SetupProxy(nil)
			listeners := cg.Listeners(proxy)

			// Find the listener for the specified port
			var targetListener *listener.Listener
			searchPort := fmt.Sprintf("0.0.0.0_%d", tc.port)
			for _, l := range listeners {
				if strings.Contains(l.Name, searchPort) {
					targetListener = l
					break
				}
			}

			if tc.expectHTTPFilter || tc.expectSNIFilter {
				g.Expect(targetListener).NotTo(BeNil(), "Should find listener for port %d", tc.port)
			}

			// Validate HTTP DFP filter
			if tc.expectHTTPFilter {
				foundHTTPDFP := false
				for _, fc := range targetListener.FilterChains {
					hcm := xdstest.ExtractHTTPConnectionManager(t, fc)
					if hcm != nil {
						for _, filter := range hcm.HttpFilters {
							if filter.Name == "envoy.filters.http.dynamic_forward_proxy" {
								foundHTTPDFP = true

								if tc.validateFilterCfg {
									var dfpConfig dfp.FilterConfig
									err := filter.GetTypedConfig().UnmarshalTo(&dfpConfig)
									g.Expect(err).To(BeNil())
									g.Expect(dfpConfig.GetDnsCacheConfig()).NotTo(BeNil())
									g.Expect(dfpConfig.GetDnsCacheConfig().Name).To(Equal("*.svc.cluster.local_dfp_dns_cache"))
								}
								break
							}
						}
					}
					if foundHTTPDFP {
						break
					}
				}
				g.Expect(foundHTTPDFP).To(BeTrue(), "HTTP DFP filter should be present")
			} else if targetListener != nil {
				// Verify HTTP DFP filter is NOT present
				for _, fc := range targetListener.FilterChains {
					hcm := xdstest.ExtractHTTPConnectionManager(t, fc)
					if hcm != nil {
						for _, filter := range hcm.HttpFilters {
							g.Expect(filter.Name).NotTo(Equal("envoy.filters.http.dynamic_forward_proxy"),
								"HTTP DFP filter should not be present")
						}
					}
				}
			}

			// Validate SNI DFP filter
			if tc.expectSNIFilter {
				foundSNIDFP := false
				for _, fc := range targetListener.FilterChains {
					for _, f := range fc.Filters {
						if f.Name == wellknown.SNIDynamicForwardProxy {
							foundSNIDFP = true

							if tc.validateFilterCfg {
								var sniDFPConfig snidfp.FilterConfig
								err := f.GetTypedConfig().UnmarshalTo(&sniDFPConfig)
								g.Expect(err).To(BeNil())
								g.Expect(sniDFPConfig.GetDnsCacheConfig()).NotTo(BeNil())
								g.Expect(sniDFPConfig.GetDnsCacheConfig().Name).To(Equal("*.svc.cluster.local_dfp_dns_cache"))
								g.Expect(sniDFPConfig.GetDnsCacheConfig().DnsLookupFamily).To(Equal(cluster.Cluster_V4_ONLY))
								g.Expect(sniDFPConfig.GetPortValue()).To(Equal(uint32(443)))
							}
							break
						}
					}
					if foundSNIDFP {
						break
					}
				}
				g.Expect(foundSNIDFP).To(BeTrue(), "SNI DFP filter should be present")
			} else if targetListener != nil {
				// Verify SNI DFP filter is NOT present
				for _, fc := range targetListener.FilterChains {
					for _, f := range fc.Filters {
						g.Expect(f.Name).NotTo(Equal(wellknown.SNIDynamicForwardProxy),
							"SNI DFP filter should not be present")
					}
				}
			}
		})
	}
}

// TestGatewayDynamicDNSFilters tests SNI DFP filter creation for a classic gateway (Router) proxy
// routing TLS passthrough traffic to a wildcard DYNAMIC_DNS ServiceEntry.
func TestGatewayDynamicDNSFilters(t *testing.T) {
	const wildcardHost = "*.destination1.com"
	configs := []config.Config{
		{
			Meta: config.Meta{GroupVersionKind: gvk.ServiceEntry, Name: "wildcard-se", Namespace: "default"},
			Spec: &networking.ServiceEntry{
				Hosts:      []string{wildcardHost},
				Ports:      []*networking.ServicePort{{Number: 443, Name: "tls", Protocol: "TLS"}},
				Location:   networking.ServiceEntry_MESH_EXTERNAL,
				Resolution: networking.ServiceEntry_DYNAMIC_DNS,
			},
		},
		{
			Meta: config.Meta{GroupVersionKind: gvk.Gateway, Name: "egress-gw", Namespace: "default"},
			Spec: &networking.Gateway{
				Selector: map[string]string{"istio": "egressgateway"},
				Servers: []*networking.Server{{
					Port:  &networking.Port{Number: 443, Name: "tls", Protocol: "TLS"},
					Hosts: []string{wildcardHost},
					Tls:   &networking.ServerTLSSettings{Mode: networking.ServerTLSSettings_PASSTHROUGH},
				}},
			},
		},
		{
			Meta: config.Meta{GroupVersionKind: gvk.VirtualService, Name: "wildcard-vs", Namespace: "default"},
			Spec: &networking.VirtualService{
				Hosts:    []string{wildcardHost},
				Gateways: []string{"egress-gw"},
				Tls: []*networking.TLSRoute{{
					Match: []*networking.TLSMatchAttributes{{
						Port:     443,
						SniHosts: []string{wildcardHost},
						Gateways: []string{"egress-gw"},
					}},
					Route: []*networking.RouteDestination{{
						Destination: &networking.Destination{Host: wildcardHost, Port: &networking.PortSelector{Number: 443}},
					}},
				}},
			},
		},
	}

	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("flag=%v", enabled), func(t *testing.T) {
			g := NewWithT(t)
			test.SetForTest(t, &features.EnableWildcardHostServiceEntriesForTLS, enabled)

			cg := NewConfigGenTest(t, TestOptions{Configs: configs})
			proxy := cg.SetupProxy(&model.Proxy{
				Type:     model.Router,
				Labels:   map[string]string{"istio": "egressgateway"},
				Metadata: &model.NodeMetadata{Labels: map[string]string{"istio": "egressgateway"}},
			})

			var sniDFP *snidfp.FilterConfig
			var sniDFPChain *listener.FilterChain
			for _, l := range cg.Listeners(proxy) {
				for _, fc := range l.FilterChains {
					for _, f := range fc.Filters {
						if f.Name == wellknown.SNIDynamicForwardProxy {
							sniDFP = &snidfp.FilterConfig{}
							sniDFPChain = fc
							g.Expect(f.GetTypedConfig().UnmarshalTo(sniDFP)).To(Succeed())
						}
					}
				}
			}

			if !enabled {
				g.Expect(sniDFP).To(BeNil(), "SNI DFP filter must be absent when the flag is off")
				return
			}
			g.Expect(sniDFP).NotTo(BeNil(), "SNI DFP filter should be present on the gateway")
			// The chain only matches SNIs under the wildcard host, so other hosts never reach the DFP filter.
			g.Expect(sniDFPChain.GetFilterChainMatch().GetServerNames()).To(ConsistOf(wildcardHost))
			g.Expect(sniDFP.GetDnsCacheConfig().Name).To(Equal(wildcardHost + "_dfp_dns_cache"))
			g.Expect(sniDFP.GetPortValue()).To(Equal(uint32(443)))
		})
	}
}

func TestGatewaySNIDFPFilter(t *testing.T) {
	route := func(host string, port uint32, weight int32) *networking.RouteDestination {
		d := &networking.RouteDestination{Destination: &networking.Destination{Host: host}, Weight: weight}
		if port != 0 {
			d.Destination.Port = &networking.PortSelector{Number: port}
		}
		return d
	}
	cases := []struct {
		name       string
		flag       bool
		serverPort int // 443 if unset
		routes     []*networking.RouteDestination
		wantPort   uint32 // 0 means no SNI DFP filter expected
	}{
		{
			name:     "wildcard DYNAMIC_DNS destination",
			flag:     true,
			routes:   []*networking.RouteDestination{route("*.wildcard.com", 443, 0)},
			wantPort: 443,
		},
		{
			name:     "destination port differs from server port",
			flag:     true,
			routes:   []*networking.RouteDestination{route("*.wildcard.com", 8443, 0)},
			wantPort: 8443,
		},
		{
			name:       "no destination port, server port differs from ServiceEntry port",
			flag:       true,
			serverPort: 8443,
			routes:     []*networking.RouteDestination{route("*.wildcard.com", 0, 0)},
			wantPort:   443,
		},
		{
			name:   "flag off",
			routes: []*networking.RouteDestination{route("*.wildcard.com", 443, 0)},
		},
		{
			name:   "ordinary destination",
			flag:   true,
			routes: []*networking.RouteDestination{route("example.com", 443, 0)},
		},
		{
			// Weighted clusters are an intentional non-goal: one DNS cache cannot serve several destinations.
			name:   "weighted destinations",
			flag:   true,
			routes: []*networking.RouteDestination{route("*.wildcard.com", 443, 50), route("*.wildcard.com", 443, 50)},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			test.SetForTest(t, &features.EnableWildcardHostServiceEntriesForTLS, tt.flag)
			wildcard := buildServiceWithPort("*.wildcard.com", 443, protocol.TLS, time.Now())
			wildcard.Resolution = model.DynamicDNS
			cg := NewConfigGenTest(t, TestOptions{
				Services: []*model.Service{wildcard, buildServiceWithPort("example.com", 443, protocol.TLS, time.Now())},
			})
			lb := ListenerBuilder{node: cg.SetupProxy(&model.Proxy{Type: model.Router}), push: cg.PushContext()}
			serverPort := tt.serverPort
			if serverPort == 0 {
				serverPort = 443
			}
			port := &model.Port{Port: serverPort, Protocol: protocol.TLS}
			base := lb.buildOutboundNetworkFilters(tt.routes, port, config.Meta{Name: "vs", Namespace: "default"}, false)

			got := lb.withGatewaySNIDFPFilter(slices.Clone(base), tt.routes, port)

			if tt.wantPort == 0 {
				g.Expect(got).To(Equal(base), "filters must be unchanged")
				return
			}
			// Exactly one filter is added, directly before the TCP proxy; all other filters are untouched.
			g.Expect(got).To(HaveLen(len(base) + 1))
			i := slices.IndexFunc(got, func(f *listener.Filter) bool { return f.Name == wellknown.SNIDynamicForwardProxy })
			g.Expect(i).To(BeNumerically(">=", 0))
			g.Expect(got[i+1].Name).To(Equal(wellknown.TCPProxy))
			g.Expect(slices.Delete(slices.Clone(got), i)).To(Equal(base))

			var cfg snidfp.FilterConfig
			g.Expect(got[i].GetTypedConfig().UnmarshalTo(&cfg)).To(Succeed())
			g.Expect(cfg.GetDnsCacheConfig().Name).To(Equal("*.wildcard.com_dfp_dns_cache"))
			g.Expect(cfg.GetPortValue()).To(Equal(tt.wantPort))
		})
	}
}

// TestGatewayMutualTLSWildcardDynamicDNS covers an ISTIO_MUTUAL gateway server whose TCP route sends to a wildcard
// DYNAMIC_DNS ServiceEntry.
func TestGatewayMutualTLSWildcardDynamicDNS(t *testing.T) {
	serviceEntry := func(name, host string) config.Config {
		return config.Config{
			Meta: config.Meta{GroupVersionKind: gvk.ServiceEntry, Name: name, Namespace: "istio-system"},
			Spec: &networking.ServiceEntry{
				Hosts:      []string{host},
				Ports:      []*networking.ServicePort{{Number: 443, Name: "tls", Protocol: "TLS"}},
				Location:   networking.ServiceEntry_MESH_EXTERNAL,
				Resolution: networking.ServiceEntry_DYNAMIC_DNS,
			},
		}
	}
	server := func(name, host string, port uint32) *networking.Server {
		return &networking.Server{
			Port:  &networking.Port{Number: port, Name: name, Protocol: "TLS"},
			Hosts: []string{host},
			Tls:   &networking.ServerTLSSettings{Mode: networking.ServerTLSSettings_ISTIO_MUTUAL},
		}
	}
	// destinationPort 0 leaves the route port unset.
	gatewayRoute := func(name, host string, serverPort, destinationPort uint32) config.Config {
		route := &networking.RouteDestination{Destination: &networking.Destination{Host: host}}
		if destinationPort != 0 {
			route.Destination.Port = &networking.PortSelector{Number: destinationPort}
		}
		return config.Config{
			Meta: config.Meta{GroupVersionKind: gvk.VirtualService, Name: name, Namespace: "istio-system"},
			Spec: &networking.VirtualService{
				Hosts:    []string{host},
				Gateways: []string{"egressgateway"},
				Tcp: []*networking.TCPRoute{{
					Match: []*networking.L4MatchAttributes{{Port: serverPort, Gateways: []string{"egressgateway"}}},
					Route: []*networking.RouteDestination{route},
				}},
			},
		}
	}
	allow := func(principal, sni string) *security.Rule {
		return &security.Rule{
			From: []*security.Rule_From{{Source: &security.Source{Principals: []string{principal}}}},
			When: []*security.Condition{{Key: "connection.sni", Values: []string{sni}}},
		}
	}
	configs := []config.Config{
		serviceEntry("wikipedia", "*.wikipedia.org"),
		serviceEntry("github", "*.github.com"),
		serviceEntry("example", "*.example.org"),
		{
			Meta: config.Meta{GroupVersionKind: gvk.Gateway, Name: "egressgateway", Namespace: "istio-system"},
			Spec: &networking.Gateway{
				Selector: map[string]string{"istio": "egressgateway"},
				Servers: []*networking.Server{
					server("tls-wikipedia", "*.wikipedia.org", 443),
					server("tls-github", "*.github.com", 443),
					// Server port differs from the ServiceEntry port; the route has no port.
					server("tls-example", "*.example.org", 8443),
				},
			},
		},
		gatewayRoute("wikipedia", "*.wikipedia.org", 443, 443),
		gatewayRoute("github", "*.github.com", 443, 443),
		gatewayRoute("example", "*.example.org", 8443, 0),
		{
			Meta: config.Meta{GroupVersionKind: gvk.AuthorizationPolicy, Name: "egress-per-caller", Namespace: "istio-system"},
			Spec: &security.AuthorizationPolicy{
				Selector: &v1beta1.WorkloadSelector{MatchLabels: map[string]string{"istio": "egressgateway"}},
				Action:   security.AuthorizationPolicy_ALLOW,
				Rules: []*security.Rule{
					allow("cluster.local/ns/svc1/sa/default", "*.wikipedia.org"),
					allow("cluster.local/ns/svc2/sa/default", "*.github.com"),
				},
			},
		},
	}

	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("flag=%v", enabled), func(t *testing.T) {
			g := NewWithT(t)
			test.SetForTest(t, &features.EnableWildcardHostServiceEntriesForTLS, enabled)
			cg := NewConfigGenTest(t, TestOptions{Configs: configs})
			proxy := cg.SetupProxy(&model.Proxy{
				Type:     model.Router,
				Labels:   map[string]string{"istio": "egressgateway"},
				Metadata: &model.NodeMetadata{Labels: map[string]string{"istio": "egressgateway"}, Namespace: "istio-system"},
			})
			listeners := cg.Listeners(proxy)
			clusters := cg.Clusters(proxy)
			xdstest.ValidateListeners(t, listeners)
			xdstest.ValidateClusters(t, clusters)

			for _, wildcard := range []string{"*.wikipedia.org", "*.github.com", "*.example.org"} {
				dfpCluster := "outbound|443||" + wildcard
				outer := findMutualTLSChain(t, listeners, wildcard)
				g.Expect(outer).NotTo(BeNil(), "ISTIO_MUTUAL chain for %s", wildcard)
				outerTCP := xdstest.ExtractTCPProxy(t, outer)

				if !enabled {
					g.Expect(outerTCP.GetCluster()).To(Equal(dfpCluster))
					g.Expect(filterNames(outer)).NotTo(ContainElement(wellknown.SNIDynamicForwardProxy))
					continue
				}

				// Outer chain: no RBAC, hands off the caller identity and original addresses.
				g.Expect(filterNames(outer)).NotTo(ContainElement(wellknown.RoleBasedAccessControl))
				g.Expect(handedOffKeys(outer)).To(ContainElements(
					"io.istio.peer_principal",
					"envoy.filters.listener.original_dst.remote_ip",
					"envoy.filters.listener.original_dst.local_ip",
				))

				g.Expect(outerTCP.GetCluster()).NotTo(Equal(dfpCluster))
				internalName := internalListenerTarget(clusters, outerTCP.GetCluster())
				g.Expect(internalName).NotTo(BeEmpty(), "cluster %s must point to an internal listener", outerTCP.GetCluster())
				// The name follows the standard cluster key format, so tools such as istioctl can parse it.
				dir, subset, hostname, port := model.ParseSubsetKey(internalName)
				g.Expect(dir).To(Equal(model.TrafficDirectionOutboundWildcardTLS))
				g.Expect(subset).To(BeEmpty())
				g.Expect(hostname).To(Equal(host.Name(wildcard)))
				g.Expect(port).To(Equal(443))
				inner := xdstest.ExtractListener(internalName, listeners)
				g.Expect(inner).NotTo(BeNil())
				g.Expect(inner.GetInternalListener()).NotTo(BeNil())
				g.Expect(xdstest.ExtractListenerFilters(inner)).To(HaveKey(wellknown.OriginalDestination))
				g.Expect(xdstest.ExtractListenerFilters(inner)).To(HaveKey(wellknown.TLSInspector))

				g.Expect(inner.FilterChains).To(HaveLen(1))
				innerChain := inner.FilterChains[0]
				g.Expect(innerChain.GetFilterChainMatch().GetServerNames()).To(ConsistOf(wildcard))

				names := filterNames(innerChain)
				rbacIdx := slices.Index(names, wellknown.RoleBasedAccessControl)
				dfpIdx := slices.Index(names, wellknown.SNIDynamicForwardProxy)
				tcpIdx := slices.Index(names, wellknown.TCPProxy)
				g.Expect(rbacIdx).To(BeNumerically(">=", 0), "inner filters %v", names)
				g.Expect(dfpIdx).To(BeNumerically(">", rbacIdx), "inner filters %v", names)
				g.Expect(tcpIdx).To(BeNumerically(">", dfpIdx), "inner filters %v", names)
				g.Expect(xdstest.ExtractTCPProxy(t, innerChain).GetCluster()).To(Equal(dfpCluster))
				var sniDFP snidfp.FilterConfig
				g.Expect(innerChain.Filters[dfpIdx].GetTypedConfig().UnmarshalTo(&sniDFP)).To(Succeed())
				g.Expect(sniDFP.GetPortValue()).To(Equal(uint32(443)))

				// Principals come from filter state; the host is the inner SNI.
				rbacJSON := rbacFilterJSON(t, innerChain)
				g.Expect(rbacJSON).To(ContainSubstring(`"key":"io.istio.peer_principal"`))
				g.Expect(rbacJSON).To(ContainSubstring(`"requestedServerName"`))
				g.Expect(rbacJSON).NotTo(ContainSubstring(`"authenticated"`))
			}
		})
	}
}

func findMutualTLSChain(t *testing.T, listeners []*listener.Listener, sni string) *listener.FilterChain {
	for _, l := range listeners {
		for _, fc := range l.FilterChains {
			if fc.GetTransportSocket() == nil || !slices.Contains(fc.GetFilterChainMatch().GetServerNames(), sni) {
				continue
			}
			ctx := &tls.DownstreamTlsContext{}
			if err := fc.GetTransportSocket().GetTypedConfig().UnmarshalTo(ctx); err != nil {
				t.Fatal(err)
			}
			if ctx.GetRequireClientCertificate().GetValue() {
				return fc
			}
		}
	}
	return nil
}

func filterNames(fc *listener.FilterChain) []string {
	return slices.Map(fc.Filters, func(f *listener.Filter) string { return f.Name })
}

// handedOffKeys returns the filter-state keys that the chain sets after the downstream TLS handshake.
func handedOffKeys(fc *listener.FilterChain) []string {
	var keys []string
	for _, f := range fc.Filters {
		cfg := &sfsnetwork.Config{}
		if f.GetTypedConfig().UnmarshalTo(cfg) != nil {
			continue
		}
		for _, v := range cfg.GetOnDownstreamTlsHandshake() {
			keys = append(keys, v.GetObjectKey())
		}
	}
	return keys
}

// internalListenerTarget returns the internal listener that the named cluster sends to, if any.
func internalListenerTarget(clusters []*cluster.Cluster, name string) string {
	c := xdstest.ExtractCluster(name, clusters)
	for _, lle := range c.GetLoadAssignment().GetEndpoints() {
		for _, ep := range lle.GetLbEndpoints() {
			if addr := ep.GetEndpoint().GetAddress().GetEnvoyInternalAddress(); addr != nil {
				return addr.GetServerListenerName()
			}
		}
	}
	return ""
}

func rbacFilterJSON(t *testing.T, fc *listener.FilterChain) string {
	for _, f := range fc.Filters {
		if f.Name != wellknown.RoleBasedAccessControl {
			continue
		}
		cfg := &rbacnetwork.RBAC{}
		if err := f.GetTypedConfig().UnmarshalTo(cfg); err != nil {
			t.Fatal(err)
		}
		js, err := protomarshal.ToJSON(cfg)
		if err != nil {
			t.Fatal(err)
		}
		return js
	}
	return ""
}
