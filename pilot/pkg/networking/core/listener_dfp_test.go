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
	snidfp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/sni_dynamic_forward_proxy/v3"
	. "github.com/onsi/gomega"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
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
		return &networking.RouteDestination{
			Destination: &networking.Destination{Host: host, Port: &networking.PortSelector{Number: port}},
			Weight:      weight,
		}
	}
	cases := []struct {
		name     string
		flag     bool
		routes   []*networking.RouteDestination
		wantPort uint32 // 0 means no SNI DFP filter expected
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
			port := &model.Port{Port: 443, Protocol: protocol.TLS}
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
