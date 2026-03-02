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

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	dfp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/dynamic_forward_proxy/v3"
	snidfp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/sni_dynamic_forward_proxy/v3"
	. "github.com/onsi/gomega"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
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
