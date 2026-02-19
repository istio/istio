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
	"testing"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	dfpcluster "github.com/envoyproxy/go-control-plane/envoy/extensions/clusters/dynamic_forward_proxy/v3"
	upstream "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	. "github.com/onsi/gomega"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
)

// TestSidecarDynamicDNSClusters tests DFP cluster creation for sidecar proxies
func TestSidecarDynamicDNSClusters(t *testing.T) {
	cases := []struct {
		name                    string
		protocol                string
		port                    uint32
		destinationRule         *networking.DestinationRule
		expectCluster           bool
		validateTLS             bool
		expectAutoSni           bool
		expectAutoSanValidation bool
		expectTransportTLS      bool
	}{
		{
			name:          "HTTP protocol",
			protocol:      "HTTP",
			port:          80,
			expectCluster: true,
		},
		{
			name:     "HTTP with ISTIO_MUTUAL mode",
			protocol: "HTTP",
			port:     80,
			destinationRule: &networking.DestinationRule{
				Host: "*.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					Tls: &networking.ClientTLSSettings{
						Mode: networking.ClientTLSSettings_ISTIO_MUTUAL,
					},
				},
			},
			expectCluster:           true,
			validateTLS:             true,
			expectAutoSni:           true,
			expectAutoSanValidation: true,
			expectTransportTLS:      true,
		},
		{
			name:          "GRPC protocol",
			protocol:      "GRPC",
			port:          9090,
			expectCluster: true,
		},
		{
			name:          "TLS protocol",
			protocol:      "TLS",
			port:          443,
			expectCluster: true,
		},
		{
			name:     "TLS with SIMPLE mode",
			protocol: "TLS",
			port:     443,
			destinationRule: &networking.DestinationRule{
				Host: "*.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					Tls: &networking.ClientTLSSettings{
						Mode: networking.ClientTLSSettings_SIMPLE,
					},
				},
			},
			expectCluster:           true,
			validateTLS:             true,
			expectAutoSni:           true,
			expectAutoSanValidation: true,
			expectTransportTLS:      true,
		},
		{
			name:     "TLS with ISTIO_MUTUAL mode",
			protocol: "TLS",
			port:     443,
			destinationRule: &networking.DestinationRule{
				Host: "*.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					Tls: &networking.ClientTLSSettings{
						Mode: networking.ClientTLSSettings_ISTIO_MUTUAL,
					},
				},
			},
			expectCluster:           true,
			validateTLS:             true,
			expectAutoSni:           true,
			expectAutoSanValidation: true, // set unless ServiceEntry annotation networking.istio.io/disableAutoSanValidation disables it
			expectTransportTLS:      true,
		},
		{
			name:     "TLS with MUTUAL mode",
			protocol: "TLS",
			port:     443,
			destinationRule: &networking.DestinationRule{
				Host: "*.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{
					Tls: &networking.ClientTLSSettings{
						Mode:              networking.ClientTLSSettings_MUTUAL,
						ClientCertificate: "/etc/certs/cert.pem",
						PrivateKey:        "/etc/certs/key.pem",
						CaCertificates:    "/etc/certs/ca.pem",
					},
				},
			},
			expectCluster:           true,
			validateTLS:             true,
			expectAutoSni:           true,
			expectAutoSanValidation: true,
			expectTransportTLS:      true,
		},
		{
			name:          "Unsupported protocol TCP",
			protocol:      "TCP",
			port:          3306,
			expectCluster: false,
		},
		{
			name:          "Unsupported protocol HTTPS",
			protocol:      "HTTPS",
			port:          443,
			expectCluster: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			serviceEntry := config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.ServiceEntry,
					Name:             "wildcard-se",
					Namespace:        "default",
				},
				Spec: &networking.ServiceEntry{
					Hosts: []string{"*.svc.cluster.local"},
					Ports: []*networking.ServicePort{
						{Number: tc.port, Name: "test-port", Protocol: tc.protocol},
					},
					Location:   networking.ServiceEntry_MESH_EXTERNAL,
					Resolution: networking.ServiceEntry_DYNAMIC_DNS,
				},
			}

			configs := []config.Config{serviceEntry}
			if tc.destinationRule != nil {
				drConfig := config.Config{
					Meta: config.Meta{
						GroupVersionKind: gvk.DestinationRule,
						Name:             "wildcard-dr",
						Namespace:        "default",
					},
					Spec: tc.destinationRule,
				}
				configs = append(configs, drConfig)
			}

			cg := NewConfigGenTest(t, TestOptions{Configs: configs})
			proxy := cg.SetupProxy(nil)
			clusters := cg.Clusters(proxy)
			xdstest.ValidateClusters(t, clusters)

			clusterMap := xdstest.ExtractClusters(clusters)
			clusterName := fmt.Sprintf("outbound|%d||*.svc.cluster.local", tc.port)
			dfpCluster, exists := clusterMap[clusterName]

			if tc.expectCluster {
				g.Expect(exists).To(BeTrue(), "DFP cluster should be created")
				g.Expect(dfpCluster).NotTo(BeNil())

				// Verify it's a DFP cluster with CLUSTER_PROVIDED LB policy
				g.Expect(dfpCluster.LbPolicy).To(Equal(cluster.Cluster_CLUSTER_PROVIDED))
				clusterType, ok := dfpCluster.ClusterDiscoveryType.(*cluster.Cluster_ClusterType)
				g.Expect(ok).To(BeTrue())
				g.Expect(clusterType.ClusterType.Name).To(Equal("envoy.clusters.dynamic_forward_proxy"))

				// Verify DNS cache config
				dfpConfig := &dfpcluster.ClusterConfig{}
				err := clusterType.ClusterType.TypedConfig.UnmarshalTo(dfpConfig)
				g.Expect(err).To(BeNil())
				g.Expect(dfpConfig.GetDnsCacheConfig()).NotTo(BeNil())
				g.Expect(dfpConfig.GetDnsCacheConfig().Name).To(Equal("*.svc.cluster.local_dfp_dns_cache"))
				g.Expect(dfpConfig.GetDnsCacheConfig().DnsLookupFamily).To(Equal(cluster.Cluster_V4_ONLY))

				// Validate TLS settings if needed
				if tc.validateTLS {
					if tc.expectTransportTLS {
						g.Expect(dfpCluster.TransportSocket).NotTo(BeNil())
						g.Expect(dfpCluster.TransportSocket.Name).To(Equal("envoy.transport_sockets.tls"))
					}

					// For TLS with DestinationRule, validate AutoSni/AutoSanValidation if HTTP protocol options are present
					if (tc.expectAutoSni || tc.expectAutoSanValidation) && dfpCluster.TypedExtensionProtocolOptions != nil {
						httpOptsAny, hasHTTPOpts := dfpCluster.TypedExtensionProtocolOptions["envoy.extensions.upstreams.http.v3.HttpProtocolOptions"]
						if hasHTTPOpts {
							httpOpts := &upstream.HttpProtocolOptions{}
							err := httpOptsAny.UnmarshalTo(httpOpts)
							g.Expect(err).To(BeNil())
							if httpOpts.UpstreamHttpProtocolOptions != nil {
								if tc.expectAutoSni {
									g.Expect(httpOpts.UpstreamHttpProtocolOptions.AutoSni).To(BeTrue())
								}
								if tc.expectAutoSanValidation {
									g.Expect(httpOpts.UpstreamHttpProtocolOptions.AutoSanValidation).To(BeTrue())
								} else {
									// Explicitly verify it's NOT set when not expected
									g.Expect(httpOpts.UpstreamHttpProtocolOptions.AutoSanValidation).To(BeFalse())
								}
							}
						}
					}
				}
			} else {
				g.Expect(exists).To(BeFalse(), "DFP cluster should not be created")
			}
		})
	}
}

func TestSidecarDynamicDNSInsecureSkipVerify(t *testing.T) {
	g := NewWithT(t)

	serviceEntry := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.ServiceEntry,
			Name:             "wildcard-se",
			Namespace:        "default",
		},
		Spec: &networking.ServiceEntry{
			Hosts: []string{"*.svc.cluster.local"},
			Ports: []*networking.ServicePort{
				{Number: 443, Name: "tls", Protocol: "TLS"},
			},
			Location:   networking.ServiceEntry_MESH_EXTERNAL,
			Resolution: networking.ServiceEntry_DYNAMIC_DNS,
		},
	}

	destinationRule := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.DestinationRule,
			Name:             "wildcard-dr",
			Namespace:        "default",
		},
		Spec: &networking.DestinationRule{
			Host: "*.svc.cluster.local",
			TrafficPolicy: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode:               networking.ClientTLSSettings_SIMPLE,
					InsecureSkipVerify: wrappers.Bool(true),
				},
			},
		},
	}

	cg := NewConfigGenTest(t, TestOptions{
		Configs: []config.Config{serviceEntry, destinationRule},
	})

	proxy := cg.SetupProxy(nil)
	clusters := cg.Clusters(proxy)
	xdstest.ValidateClusters(t, clusters)

	clusterMap := xdstest.ExtractClusters(clusters)
	dfpCluster := clusterMap["outbound|443||*.svc.cluster.local"]
	g.Expect(dfpCluster).NotTo(BeNil(), "DFP cluster should be created")

	// Verify it's a DFP cluster
	clusterType, ok := dfpCluster.ClusterDiscoveryType.(*cluster.Cluster_ClusterType)
	g.Expect(ok).To(BeTrue())
	g.Expect(clusterType.ClusterType.Name).To(Equal("envoy.clusters.dynamic_forward_proxy"))

	// Verify InsecureSkipVerify sets AllowInsecureClusterOptions
	dfpConfig := &dfpcluster.ClusterConfig{}
	err := clusterType.ClusterType.TypedConfig.UnmarshalTo(dfpConfig)
	g.Expect(err).To(BeNil())
	g.Expect(dfpConfig.AllowInsecureClusterOptions).To(BeTrue(),
		"AllowInsecureClusterOptions should be true when InsecureSkipVerify is set")

	// Verify TLS transport socket is still present
	g.Expect(dfpCluster.TransportSocket).NotTo(BeNil())
	g.Expect(dfpCluster.TransportSocket.Name).To(Equal("envoy.transport_sockets.tls"))

	// Verify AutoSni and AutoSanValidation are NOT set when InsecureSkipVerify is true
	if dfpCluster.TypedExtensionProtocolOptions != nil {
		httpOptsAny, hasHTTPOpts := dfpCluster.TypedExtensionProtocolOptions["envoy.extensions.upstreams.http.v3.HttpProtocolOptions"]
		if hasHTTPOpts {
			httpOpts := &upstream.HttpProtocolOptions{}
			err := httpOptsAny.UnmarshalTo(httpOpts)
			g.Expect(err).To(BeNil())
			if httpOpts.UpstreamHttpProtocolOptions != nil {
				// AutoSni should be true, but AutoSanValidation should be false with InsecureSkipVerify
				g.Expect(httpOpts.UpstreamHttpProtocolOptions.AutoSni).To(BeTrue())
				g.Expect(httpOpts.UpstreamHttpProtocolOptions.AutoSanValidation).To(BeFalse(),
					"AutoSanValidation should be false when InsecureSkipVerify is true")
			}
		}
	}
}
