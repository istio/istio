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
	dfpcluster "github.com/envoyproxy/go-control-plane/envoy/extensions/clusters/dynamic_forward_proxy/v3"
	dfp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/dynamic_forward_proxy/v3"
	rbacnetwork "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/rbac/v3"
	sfsnetwork "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/set_filter_state/v3"
	snidfp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/sni_dynamic_forward_proxy/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	network "github.com/envoyproxy/go-control-plane/envoy/extensions/matching/common_inputs/network/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/types/known/durationpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	networking "istio.io/api/networking/v1alpha3"
	security "istio.io/api/security/v1beta1"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
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

// TestGatewayDynamicDNSSNIHosts tests that a TLS passthrough route to a wildcard DYNAMIC_DNS ServiceEntry only gets an
// SNI DFP chain when all its SNI hosts are under the ServiceEntry host, so the chain never forwards other SNIs.
func TestGatewayDynamicDNSSNIHosts(t *testing.T) {
	test.SetForTest(t, &features.EnableWildcardHostServiceEntriesForTLS, true)
	const wildcardHost = "*.destination1.com"
	// otherWildcardHost is a second host of the same ServiceEntry, so it is a different service.
	const otherWildcardHost = "*.destination2.com"
	virtualService := func(name string, created time.Time, hosts, sniHosts []string) config.Config {
		return config.Config{
			Meta: config.Meta{GroupVersionKind: gvk.VirtualService, Name: name, Namespace: "default", CreationTimestamp: created},
			Spec: &networking.VirtualService{
				Hosts:    hosts,
				Gateways: []string{"egress-gw"},
				Tls: []*networking.TLSRoute{{
					Match: []*networking.TLSMatchAttributes{{Port: 443, SniHosts: sniHosts}},
					Route: []*networking.RouteDestination{{
						Destination: &networking.Destination{Host: wildcardHost, Port: &networking.PortSelector{Number: 443}},
					}},
				}},
			},
		}
	}
	now := time.Now()
	cases := []struct {
		name            string
		gatewayHosts    []string // ["*"] if unset
		virtualServices []config.Config
		// wantServerNames are the server names of the SNI DFP chains.
		wantServerNames [][]string
	}{
		{
			name:            "SNI host under the wildcard host",
			virtualServices: []config.Config{virtualService("vs", now, []string{wildcardHost}, []string{"a.destination1.com"})},
			wantServerNames: [][]string{{"a.destination1.com"}},
		},
		{
			name:            "all SNI hosts",
			virtualServices: []config.Config{virtualService("vs", now, []string{"*"}, []string{"*"})},
		},
		{
			name:            "all SNI hosts, gateway server limited to the wildcard host",
			gatewayHosts:    []string{wildcardHost},
			virtualServices: []config.Config{virtualService("vs", now, []string{"*"}, []string{"*"})},
		},
		{
			name:            "wildcard host and all SNI hosts",
			virtualServices: []config.Config{virtualService("vs", now, []string{"*"}, []string{wildcardHost, "*"})},
		},
		{
			name:            "one SNI host outside the wildcard host",
			virtualServices: []config.Config{virtualService("vs", now, []string{"*"}, []string{wildcardHost, "other.com"})},
		},
		{
			name:            "unrelated SNI host",
			virtualServices: []config.Config{virtualService("vs", now, []string{"other.com"}, []string{"other.com"})},
		},
		{
			name:            "SNI host of another ServiceEntry host",
			virtualServices: []config.Config{virtualService("vs", now, []string{otherWildcardHost}, []string{otherWildcardHost})},
		},
		{
			name:            "no SNI hosts",
			virtualServices: []config.Config{virtualService("vs", now, []string{"*"}, nil)},
		},
		{
			name: "rejected route does not hide a later route with the same SNI host",
			virtualServices: []config.Config{
				virtualService("rejected", now, []string{"*"}, []string{wildcardHost, "other.com"}),
				virtualService("accepted", now.Add(time.Second), []string{wildcardHost}, []string{wildcardHost}),
			},
			wantServerNames: [][]string{{wildcardHost}},
		},
		{
			name: "rejected all-SNI route next to a route for the wildcard host",
			virtualServices: []config.Config{
				virtualService("all-sni", now, []string{"*"}, []string{"*"}),
				virtualService("wildcard", now.Add(time.Second), []string{wildcardHost}, []string{wildcardHost}),
			},
			wantServerNames: [][]string{{wildcardHost}},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			gatewayHosts := tt.gatewayHosts
			if gatewayHosts == nil {
				gatewayHosts = []string{"*"}
			}
			configs := append([]config.Config{
				{
					Meta: config.Meta{GroupVersionKind: gvk.ServiceEntry, Name: "wildcard-se", Namespace: "default"},
					Spec: &networking.ServiceEntry{
						Hosts:      []string{wildcardHost, otherWildcardHost},
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
							Hosts: gatewayHosts,
							Tls:   &networking.ServerTLSSettings{Mode: networking.ServerTLSSettings_PASSTHROUGH},
						}},
					},
				},
			}, tt.virtualServices...)
			cg := NewConfigGenTest(t, TestOptions{Configs: configs})
			proxy := cg.SetupProxy(&model.Proxy{
				Type:     model.Router,
				Labels:   map[string]string{"istio": "egressgateway"},
				Metadata: &model.NodeMetadata{Labels: map[string]string{"istio": "egressgateway"}},
			})

			listeners := cg.Listeners(proxy)
			xdstest.ValidateListeners(t, listeners)
			var serverNames [][]string
			for _, l := range listeners {
				for _, fc := range l.FilterChains {
					if slices.ContainsFunc(fc.Filters, func(f *listener.Filter) bool { return f.Name == wellknown.SNIDynamicForwardProxy }) {
						serverNames = append(serverNames, fc.GetFilterChainMatch().GetServerNames())
					}
				}
			}
			g.Expect(serverNames).To(Equal(tt.wantServerNames))
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
			// Weighted clusters are an intentional non-goal: the chain has one SNI dynamic forward proxy filter.
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

			internalClusters := slices.Filter(clusters, func(c *cluster.Cluster) bool {
				return internalListenerTarget(clusters, c.Name) != ""
			})
			if !enabled {
				g.Expect(internalClusters).To(BeEmpty())
				g.Expect(xdstest.ExtractListener(gatewayWildcardTLSInternalName, listeners)).To(BeNil())
			} else {
				// As the waypoint main_internal cluster, a single internal cluster serves all targets.
				g.Expect(slices.Map(internalClusters, func(c *cluster.Cluster) string { return c.Name })).
					To(ConsistOf(gatewayWildcardTLSInternalName))
				g.Expect(internalListenerTarget(clusters, gatewayWildcardTLSInternalName)).To(Equal(gatewayWildcardTLSInternalName))
			}
			inner := xdstest.ExtractListener(gatewayWildcardTLSInternalName, listeners)

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

				// Outer chain: no RBAC, hands off the caller identity, the original addresses and the target.
				g.Expect(filterNames(outer)).NotTo(ContainElement(wellknown.RoleBasedAccessControl))
				g.Expect(handedOffKeys(outer)).To(ContainElements(
					"io.istio.peer_principal",
					"envoy.filters.listener.original_dst.remote_ip",
					"envoy.filters.listener.original_dst.local_ip",
					xdsfilters.GatewayWildcardTLSTargetFilterStateKey,
				))
				target := wildcard + ":443"
				g.Expect(handedOffValue(outer, xdsfilters.GatewayWildcardTLSTargetFilterStateKey)).To(Equal(target))
				// No application data is read before the peer identity is known.
				g.Expect(outerTCP.GetUpstreamConnectMode()).To(Equal(tcp.UpstreamConnectMode_ON_DOWNSTREAM_TLS_HANDSHAKE))
				g.Expect(outerTCP.GetMaxEarlyDataBytes().GetValue()).To(BeZero())
				g.Expect(outerTCP.GetCluster()).To(Equal(gatewayWildcardTLSInternalName))

				g.Expect(inner).NotTo(BeNil())
				g.Expect(inner.GetInternalListener()).NotTo(BeNil())
				g.Expect(xdstest.ExtractListenerFilters(inner)).To(HaveKey(wellknown.OriginalDestination))
				g.Expect(xdstest.ExtractListenerFilters(inner)).To(HaveKey(wellknown.TLSInspector))

				// The chain is selected by the target, then limited to SNIs under the wildcard host.
				innerChain, suffix := gatewayWildcardTLSChain(t, inner, target)
				g.Expect(innerChain).NotTo(BeNil(), "internal chain for %s", target)
				g.Expect(suffix).To(Equal(wildcard[1:]))
				g.Expect(innerChain.GetFilterChainMatch()).To(BeNil())
				// The chain name follows the standard cluster key format, so tools such as istioctl can parse it.
				dir, subset, hostname, port := model.ParseSubsetKey(innerChain.Name)
				g.Expect(dir).To(Equal(model.TrafficDirectionOutboundWildcardTLS))
				g.Expect(subset).To(BeEmpty())
				g.Expect(hostname).To(Equal(host.Name(wildcard)))
				g.Expect(port).To(Equal(443))

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
			if enabled {
				g.Expect(inner.FilterChains).To(HaveLen(3))
				// A connection with an unknown or missing target matches no chain and is closed.
				g.Expect(inner.GetFilterChainMatcher().GetOnNoMatch()).To(BeNil())
				g.Expect(inner.GetDefaultFilterChain()).To(BeNil())
			}
		})
	}
}

// TestGatewayWildcardDynamicDNSSubsets tests that a gateway route to a subset of a wildcard DYNAMIC_DNS ServiceEntry
// sends to a dynamic forward proxy cluster of that subset, as on waypoints.
func TestGatewayWildcardDynamicDNSSubsets(t *testing.T) {
	const (
		wildcard = "*.wikipedia.org"
		base     = "outbound|443||*.wikipedia.org"
		v1       = "outbound|443|v1|*.wikipedia.org"
		v2       = "outbound|443|v2|*.wikipedia.org"
	)
	serviceEntry := config.Config{
		Meta: config.Meta{GroupVersionKind: gvk.ServiceEntry, Name: "wikipedia", Namespace: "istio-system"},
		Spec: &networking.ServiceEntry{
			Hosts:      []string{wildcard},
			Ports:      []*networking.ServicePort{{Number: 443, Name: "tls", Protocol: "TLS"}},
			Location:   networking.ServiceEntry_MESH_EXTERNAL,
			Resolution: networking.ServiceEntry_DYNAMIC_DNS,
		},
	}
	// Subset v1 has its own traffic policy.
	destinationRule := func(subsets ...string) config.Config {
		dr := &networking.DestinationRule{Host: wildcard}
		for _, name := range subsets {
			subset := &networking.Subset{Name: name}
			if name == "v1" {
				subset.TrafficPolicy = &networking.TrafficPolicy{
					OutlierDetection: &networking.OutlierDetection{Consecutive_5XxErrors: wrappers.UInt32(3)},
				}
			}
			dr.Subsets = append(dr.Subsets, subset)
		}
		return config.Config{
			Meta: config.Meta{GroupVersionKind: gvk.DestinationRule, Name: "wikipedia", Namespace: "istio-system"},
			Spec: dr,
		}
	}
	gateway := func(mode networking.ServerTLSSettings_TLSmode, ports ...uint32) config.Config {
		gw := &networking.Gateway{Selector: map[string]string{"istio": "egressgateway"}}
		for _, port := range ports {
			gw.Servers = append(gw.Servers, &networking.Server{
				Port:  &networking.Port{Number: port, Name: fmt.Sprintf("tls-%d", port), Protocol: "TLS"},
				Hosts: []string{wildcard},
				Tls:   &networking.ServerTLSSettings{Mode: mode},
			})
		}
		return config.Config{Meta: config.Meta{GroupVersionKind: gvk.Gateway, Name: "egressgateway", Namespace: "istio-system"}, Spec: gw}
	}
	destination := func(subset string) []*networking.RouteDestination {
		return []*networking.RouteDestination{{Destination: &networking.Destination{
			Host: wildcard, Subset: subset, Port: &networking.PortSelector{Number: 443},
		}}}
	}
	passthroughRoute := config.Config{
		Meta: config.Meta{GroupVersionKind: gvk.VirtualService, Name: "wikipedia", Namespace: "istio-system"},
		Spec: &networking.VirtualService{
			Hosts:    []string{wildcard},
			Gateways: []string{"istio-system/egressgateway"},
			Tls: []*networking.TLSRoute{{
				Match: []*networking.TLSMatchAttributes{{Port: 443, SniHosts: []string{wildcard}}},
				Route: destination("v1"),
			}},
		},
	}
	// One ISTIO_MUTUAL server per subset of the same host and port.
	mutualRoute := config.Config{
		Meta: config.Meta{GroupVersionKind: gvk.VirtualService, Name: "wikipedia", Namespace: "istio-system"},
		Spec: &networking.VirtualService{
			Hosts:    []string{wildcard},
			Gateways: []string{"istio-system/egressgateway"},
			Tcp: []*networking.TCPRoute{
				{Match: []*networking.L4MatchAttributes{{Port: 443}}, Route: destination("v1")},
				{Match: []*networking.L4MatchAttributes{{Port: 8443}}, Route: destination("v2")},
			},
		},
	}
	gatewayProxy := func() *model.Proxy {
		return &model.Proxy{
			Type:     model.Router,
			Labels:   map[string]string{"istio": "egressgateway"},
			Metadata: &model.NodeMetadata{Labels: map[string]string{"istio": "egressgateway"}, Namespace: "istio-system"},
		}
	}
	dfpClusters := func(clusters []*cluster.Cluster) map[string]*cluster.Cluster {
		res := map[string]*cluster.Cluster{}
		for _, c := range clusters {
			if c.GetClusterType().GetName() == "envoy.clusters.dynamic_forward_proxy" {
				res[c.Name] = c
			}
		}
		return res
	}
	dnsCache := func(g *WithT, c *cluster.Cluster) string {
		cfg := &dfpcluster.ClusterConfig{}
		g.Expect(c.GetClusterType().GetTypedConfig().UnmarshalTo(cfg)).To(Succeed())
		return cfg.GetDnsCacheConfig().GetName()
	}
	passthroughConfigs := []config.Config{
		serviceEntry, destinationRule("v1", "v2"), gateway(networking.ServerTLSSettings_PASSTHROUGH, 443), passthroughRoute,
	}

	t.Run("subset clusters", func(t *testing.T) {
		g := NewWithT(t)
		test.SetForTest(t, &features.EnableWildcardHostServiceEntriesForTLS, true)
		cg := NewConfigGenTest(t, TestOptions{Configs: passthroughConfigs})
		clusters := cg.Clusters(cg.SetupProxy(gatewayProxy()))
		xdstest.ValidateClusters(t, clusters)
		dfp := dfpClusters(clusters)
		g.Expect(dfp).To(HaveLen(3))
		g.Expect(dfp).To(HaveKey(base))
		g.Expect(dfp).To(HaveKey(v1))
		g.Expect(dfp).To(HaveKey(v2))
		// The subsets share the DNS cache of the host.
		g.Expect(dnsCache(g, dfp[v1])).To(Equal(dnsCache(g, dfp[base])))
		g.Expect(dnsCache(g, dfp[v2])).To(Equal(dnsCache(g, dfp[base])))
		g.Expect(dfp[v1].GetMetadata().GetFilterMetadata()["istio"].GetFields()["subset"].GetStringValue()).To(Equal("v1"))
		// The subset traffic policy applies only to its cluster.
		g.Expect(dfp[v1].GetOutlierDetection().GetConsecutive_5Xx().GetValue()).To(Equal(uint32(3)))
		g.Expect(dfp[base].GetOutlierDetection()).To(BeNil())
		g.Expect(dfp[v2].GetOutlierDetection()).To(BeNil())
	})

	t.Run("flag off", func(t *testing.T) {
		g := NewWithT(t)
		test.SetForTest(t, &features.EnableWildcardHostServiceEntriesForTLS, false)
		cg := NewConfigGenTest(t, TestOptions{Configs: passthroughConfigs})
		dfp := dfpClusters(cg.Clusters(cg.SetupProxy(gatewayProxy())))
		g.Expect(dfp).To(HaveLen(1))
		g.Expect(dfp).To(HaveKey(base))
	})

	t.Run("sidecar", func(t *testing.T) {
		g := NewWithT(t)
		test.SetForTest(t, &features.EnableWildcardHostServiceEntriesForTLS, true)
		cg := NewConfigGenTest(t, TestOptions{Configs: []config.Config{serviceEntry, destinationRule("v1", "v2")}})
		dfp := dfpClusters(cg.Clusters(cg.SetupProxy(nil)))
		g.Expect(dfp).To(HaveLen(1))
		g.Expect(dfp).To(HaveKey(base))
	})

	t.Run("passthrough route", func(t *testing.T) {
		g := NewWithT(t)
		test.SetForTest(t, &features.EnableWildcardHostServiceEntriesForTLS, true)
		cg := NewConfigGenTest(t, TestOptions{Configs: passthroughConfigs})
		proxy := cg.SetupProxy(gatewayProxy())
		listeners := cg.Listeners(proxy)
		xdstest.ValidateListeners(t, listeners)
		dfp := dfpClusters(cg.Clusters(proxy))
		var routed []string
		for _, l := range listeners {
			for _, fc := range l.FilterChains {
				if slices.Contains(filterNames(fc), wellknown.SNIDynamicForwardProxy) {
					routed = append(routed, xdstest.ExtractTCPProxy(t, fc).GetCluster())
				}
			}
		}
		g.Expect(routed).To(Equal([]string{v1}))
		g.Expect(dfp).To(HaveKey(v1))
	})

	t.Run("istio mutual routes", func(t *testing.T) {
		g := NewWithT(t)
		test.SetForTest(t, &features.EnableWildcardHostServiceEntriesForTLS, true)
		cg := NewConfigGenTest(t, TestOptions{Configs: []config.Config{
			serviceEntry, destinationRule("v1", "v2"), gateway(networking.ServerTLSSettings_ISTIO_MUTUAL, 443, 8443), mutualRoute,
		}})
		proxy := cg.SetupProxy(gatewayProxy())
		listeners := cg.Listeners(proxy)
		xdstest.ValidateListeners(t, listeners)
		dfp := dfpClusters(cg.Clusters(proxy))
		inner := xdstest.ExtractListener(gatewayWildcardTLSInternalName, listeners)
		g.Expect(inner).NotTo(BeNil())
		// Each subset has its own chain on the internal listener, which sends to the cluster of the subset.
		g.Expect(inner.FilterChains).To(HaveLen(2))
		for subset, dfpCluster := range map[string]string{"v1": v1, "v2": v2} {
			chain, suffix := gatewayWildcardTLSChain(t, inner, wildcard+":443/"+subset)
			g.Expect(chain).NotTo(BeNil(), "internal chain for subset %s", subset)
			g.Expect(suffix).To(Equal(wildcard[1:]))
			g.Expect(chain.Name).To(Equal("outbound-wildcard-tls|443|" + subset + "|" + wildcard))
			g.Expect(xdstest.ExtractTCPProxy(t, chain).GetCluster()).To(Equal(dfpCluster))
			g.Expect(dfp).To(HaveKey(dfpCluster))
		}
	})

	t.Run("subset removed", func(t *testing.T) {
		g := NewWithT(t)
		test.SetForTest(t, &features.EnableWildcardHostServiceEntriesForTLS, true)
		cg := NewConfigGenTest(t, TestOptions{Configs: passthroughConfigs})
		proxy := cg.SetupProxy(gatewayProxy())
		next := []config.Config{
			serviceEntry, destinationRule("v1"), gateway(networking.ServerTLSSettings_PASSTHROUGH, 443), passthroughRoute,
		}
		applyConfigDiff(t, cg, passthroughConfigs, next)
		pc := model.NewPushContext()
		pc.InitContext(cg.env, nil, nil)
		cg.env.SetPushContext(pc)
		proxy.SetGatewaysForProxy(cg.env.PushContext())
		proxy.SetSidecarScope(cg.env.PushContext())
		clusters, removed, delta := cg.DeltaClusters(proxy,
			sets.New(model.ConfigKey{Kind: kind.DestinationRule, Name: "wikipedia", Namespace: "istio-system"}),
			&model.WatchedResource{ResourceNames: sets.New(base, v1, v2)})
		g.Expect(delta).To(BeTrue())
		g.Expect(removed).To(Equal([]string{v2}))
		g.Expect(dfpClusters(clusters)).To(HaveKey(v1))
	})
}

func TestGatewayMutualTLSWildcardIdleTimeout(t *testing.T) {
	const wildcard = "*.wikipedia.org"
	configs := []config.Config{
		{
			Meta: config.Meta{GroupVersionKind: gvk.ServiceEntry, Name: "wikipedia", Namespace: "istio-system"},
			Spec: &networking.ServiceEntry{
				Hosts:      []string{wildcard},
				Ports:      []*networking.ServicePort{{Number: 443, Name: "tls", Protocol: "TLS"}},
				Location:   networking.ServiceEntry_MESH_EXTERNAL,
				Resolution: networking.ServiceEntry_DYNAMIC_DNS,
			},
		},
		{
			Meta: config.Meta{GroupVersionKind: gvk.Gateway, Name: "egressgateway", Namespace: "istio-system"},
			Spec: &networking.Gateway{
				Selector: map[string]string{"istio": "egressgateway"},
				Servers: []*networking.Server{{
					Port:  &networking.Port{Number: 443, Name: "tls-wikipedia", Protocol: "TLS"},
					Hosts: []string{wildcard},
					Tls:   &networking.ServerTLSSettings{Mode: networking.ServerTLSSettings_ISTIO_MUTUAL},
				}},
			},
		},
		{
			Meta: config.Meta{GroupVersionKind: gvk.VirtualService, Name: "wikipedia", Namespace: "istio-system"},
			Spec: &networking.VirtualService{
				Hosts:    []string{wildcard},
				Gateways: []string{"egressgateway"},
				Tcp: []*networking.TCPRoute{{
					Match: []*networking.L4MatchAttributes{{Port: 443, Gateways: []string{"egressgateway"}}},
					Route: []*networking.RouteDestination{{Destination: &networking.Destination{Host: wildcard, Port: &networking.PortSelector{Number: 443}}}},
				}},
			},
		},
	}
	destinationRule := config.Config{
		Meta: config.Meta{GroupVersionKind: gvk.DestinationRule, Name: "wikipedia", Namespace: "istio-system"},
		Spec: &networking.DestinationRule{
			Host: wildcard,
			TrafficPolicy: &networking.TrafficPolicy{ConnectionPool: &networking.ConnectionPoolSettings{
				Tcp: &networking.ConnectionPoolSettings_TCPSettings{IdleTimeout: durationpb.New(2 * time.Hour)},
			}},
		},
	}

	// The internal listener owns the idle timeout, as the waypoint does. The gateway chain has none, so it cannot
	// close the connection before the timeout set for the external host.
	cases := []struct {
		name            string
		destinationRule bool
		metadataTimeout string
		// innerIdleTimeout 0 means unset, so Envoy applies its default.
		innerIdleTimeout time.Duration
	}{
		{name: "destination rule", destinationRule: true, innerIdleTimeout: 2 * time.Hour},
		{name: "proxy metadata", metadataTimeout: "3h", innerIdleTimeout: 3 * time.Hour},
		{name: "default"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			test.SetForTest(t, &features.EnableWildcardHostServiceEntriesForTLS, true)
			cfgs := configs
			if tt.destinationRule {
				cfgs = append(slices.Clone(configs), destinationRule)
			}
			cg := NewConfigGenTest(t, TestOptions{Configs: cfgs})
			proxy := cg.SetupProxy(&model.Proxy{
				Type:   model.Router,
				Labels: map[string]string{"istio": "egressgateway"},
				Metadata: &model.NodeMetadata{
					Labels:      map[string]string{"istio": "egressgateway"},
					Namespace:   "istio-system",
					IdleTimeout: tt.metadataTimeout,
				},
			})
			listeners := cg.Listeners(proxy)
			clusters := cg.Clusters(proxy)

			outer := findMutualTLSChain(t, listeners, wildcard)
			g.Expect(outer).NotTo(BeNil())
			outerTCP := xdstest.ExtractTCPProxy(t, outer)
			g.Expect(outerTCP.GetIdleTimeout()).NotTo(BeNil())
			g.Expect(outerTCP.GetIdleTimeout().AsDuration()).To(BeZero())

			inner := xdstest.ExtractListener(internalListenerTarget(clusters, outerTCP.GetCluster()), listeners)
			g.Expect(inner).NotTo(BeNil())
			innerIdleTimeout := xdstest.ExtractTCPProxy(t, inner.FilterChains[0]).GetIdleTimeout()
			if tt.innerIdleTimeout == 0 {
				g.Expect(innerIdleTimeout).To(BeNil())
			} else {
				g.Expect(innerIdleTimeout.AsDuration()).To(Equal(tt.innerIdleTimeout))
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

// handedOffValue returns the value that the chain sets for a filter-state key after the downstream TLS handshake.
func handedOffValue(fc *listener.FilterChain, key string) string {
	for _, f := range fc.Filters {
		cfg := &sfsnetwork.Config{}
		if f.GetTypedConfig().UnmarshalTo(cfg) != nil {
			continue
		}
		for _, v := range cfg.GetOnDownstreamTlsHandshake() {
			if v.GetObjectKey() == key {
				return v.GetFormatString().GetTextFormatSource().GetInlineString()
			}
		}
	}
	return ""
}

// gatewayWildcardTLSChain returns the chain of the internal listener selected for a target, and the SNI suffix
// that the chain is limited to.
func gatewayWildcardTLSChain(t *testing.T, l *listener.Listener, target string) (*listener.FilterChain, string) {
	onMatch := l.GetFilterChainMatcher().GetMatcherTree().GetExactMatchMap().GetMap()[target]
	matchers := onMatch.GetMatcher().GetMatcherList().GetMatchers()
	if len(matchers) != 1 {
		return nil, ""
	}
	predicate := matchers[0].GetPredicate().GetSinglePredicate()
	if !predicate.GetInput().GetTypedConfig().MessageIs(&network.ServerNameInput{}) {
		t.Fatalf("SNI predicate expected, got %v", predicate.GetInput())
	}
	action := &wrappers.StringValue{}
	if err := matchers[0].GetOnMatch().GetAction().GetTypedConfig().UnmarshalTo(action); err != nil {
		t.Fatal(err)
	}
	for _, fc := range l.FilterChains {
		if fc.Name == action.GetValue() {
			return fc, predicate.GetValueMatch().GetSuffix()
		}
	}
	return nil, ""
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
