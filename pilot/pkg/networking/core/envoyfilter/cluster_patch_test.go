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

package envoyfilter

import (
	"testing"

	udpa "github.com/cncf/xds/go/udpa/type/v1"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	admission "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/admission_control/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	http "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pilot/pkg/util/protoconv"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/host"
)

func Test_clusterMatch(t *testing.T) {
	type args struct {
		proxy          *model.Proxy
		cluster        *cluster.Cluster
		matchCondition *networking.EnvoyFilter_EnvoyConfigObjectMatch
		operation      networking.EnvoyFilter_Patch_Operation
		host           host.Name
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "name mismatch",
			args: args{
				proxy:     &model.Proxy{Type: model.SidecarProxy},
				operation: networking.EnvoyFilter_Patch_MERGE,
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
					ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
						Cluster: &networking.EnvoyFilter_ClusterMatch{Name: "scooby"},
					},
				},
				cluster: &cluster.Cluster{Name: "scrappy"},
			},
			want: false,
		},
		{
			name: "subset mismatch",
			args: args{
				proxy:     &model.Proxy{Type: model.SidecarProxy},
				operation: networking.EnvoyFilter_Patch_MERGE,
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
					ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
						Cluster: &networking.EnvoyFilter_ClusterMatch{
							PortNumber: 80,
							Service:    "foo.bar",
							Subset:     "v1",
						},
					},
				},
				cluster: &cluster.Cluster{Name: "outbound|80|v2|foo.bar"},
			},
			want: false,
		},
		{
			name: "service mismatch",
			args: args{
				proxy:     &model.Proxy{Type: model.SidecarProxy},
				operation: networking.EnvoyFilter_Patch_MERGE,
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
					ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
						Cluster: &networking.EnvoyFilter_ClusterMatch{
							PortNumber: 80,
							Service:    "foo.bar",
							Subset:     "v1",
						},
					},
				},
				cluster: &cluster.Cluster{Name: "outbound|80|v1|google.com"},
			},
			want: false,
		},
		{
			name: "service name match for inbound cluster",
			args: args{
				proxy:     &model.Proxy{Type: model.SidecarProxy},
				operation: networking.EnvoyFilter_Patch_MERGE,
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
					ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
						Cluster: &networking.EnvoyFilter_ClusterMatch{
							Service: "foo.bar",
						},
					},
				},
				cluster: &cluster.Cluster{Name: "inbound|80||"},
				host:    "foo.bar",
			},
			want: true,
		},
		{
			name: "port mismatch",
			args: args{
				proxy:     &model.Proxy{Type: model.SidecarProxy},
				operation: networking.EnvoyFilter_Patch_MERGE,
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
					ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
						Cluster: &networking.EnvoyFilter_ClusterMatch{
							PortNumber: 80,
							Service:    "foo.bar",
							Subset:     "v1",
						},
					},
				},
				cluster: &cluster.Cluster{Name: "outbound|90|v1|foo.bar"},
			},
			want: false,
		},
		{
			name: "full match",
			args: args{
				proxy:     &model.Proxy{Type: model.SidecarProxy},
				operation: networking.EnvoyFilter_Patch_MERGE,
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
					ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
						Cluster: &networking.EnvoyFilter_ClusterMatch{
							PortNumber: 80,
							Service:    "foo.bar",
							Subset:     "v1",
						},
					},
				},
				cluster: &cluster.Cluster{Name: "outbound|80|v1|foo.bar"},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clusterMatch(tt.args.cluster, &model.EnvoyFilterConfigPatchWrapper{Match: tt.args.matchCondition}, []host.Name{tt.args.host}); got != tt.want {
				t.Errorf("clusterMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClusterPatching(t *testing.T) {
	configPatches := []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
		{
			ApplyTo: networking.EnvoyFilter_CLUSTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name":"new-cluster1"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_CLUSTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name":"new-cluster2"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_CLUSTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_GATEWAY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
					Cluster: &networking.EnvoyFilter_ClusterMatch{
						Service: "gateway.com",
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{Operation: networking.EnvoyFilter_Patch_REMOVE},
		},
		{
			ApplyTo: networking.EnvoyFilter_CLUSTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_INBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
					Cluster: &networking.EnvoyFilter_ClusterMatch{
						PortNumber: 9999,
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{Operation: networking.EnvoyFilter_Patch_REMOVE},
		},
		{
			ApplyTo: networking.EnvoyFilter_CLUSTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_INBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
					Cluster: &networking.EnvoyFilter_ClusterMatch{
						Service: "service.servicens",
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value:     buildPatchStruct(`{"type":"EDS"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_CLUSTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_ANY,
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value:     buildPatchStruct(`{"dns_lookup_family":"V6_ONLY"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_CLUSTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_ANY,
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value:     buildPatchStruct(`{"lb_policy":"RING_HASH"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_CLUSTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
					Cluster: &networking.EnvoyFilter_ClusterMatch{
						PortNumber: 443,
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value: buildPatchStruct(`
				{"transport_socket":{
					"name":"envoy.transport_sockets.tls",
					"typed_config":{
						"@type":"type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext",
						"common_tls_context":{
							"tls_params":{
								"tls_minimum_protocol_version":"TLSv1_3"}}}}}`),
			},
		},
		// Patch custom TLS type
		{
			ApplyTo: networking.EnvoyFilter_CLUSTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
					Cluster: &networking.EnvoyFilter_ClusterMatch{
						PortNumber: 7777,
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value: buildPatchStruct(`
					{"transport_socket":{
						"name":"transport_sockets.alts",
						"typed_config":{
							"@type":"type.googleapis.com/udpa.type.v1.TypedStruct",
              "type_url": "type.googleapis.com/envoy.extensions.transport_sockets.alts.v3.Alts",
							"value":{"handshaker_service":"1.2.3.4"}}}}`),
			},
		},
		// Add upstream http filter
		{
			ApplyTo: networking.EnvoyFilter_UPSTREAM_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_GATEWAY,
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name":"http-filter1"}`),
			},
		},
		// Add upstream http filter with cluster match
		{
			ApplyTo: networking.EnvoyFilter_UPSTREAM_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_GATEWAY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
					Cluster: &networking.EnvoyFilter_ClusterMatch{
						Name: "cluster2",
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name":"http-filter2"}`),
			},
		},
		// Remove upstream http filter
		{
			ApplyTo: networking.EnvoyFilter_UPSTREAM_HTTP_FILTER,
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name":"http-filter-to-be-removed"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_UPSTREAM_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_ANY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
					Cluster: &networking.EnvoyFilter_ClusterMatch{
						Filter: &networking.EnvoyFilter_ClusterMatch_UpstreamFilterMatch{
							Name: "http-filter-to-be-removed",
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REMOVE,
			},
		},
		// Insert upstream http filter before existing filter
		{
			ApplyTo: networking.EnvoyFilter_UPSTREAM_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_GATEWAY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
					Cluster: &networking.EnvoyFilter_ClusterMatch{
						Name: "cluster2",
						Filter: &networking.EnvoyFilter_ClusterMatch_UpstreamFilterMatch{
							Name: "http-filter2",
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_INSERT_BEFORE,
				Value:     buildPatchStruct(`{"name":"http-filter3"}`),
			},
		},
		// Insert upstream http filter after existing filter
		{
			ApplyTo: networking.EnvoyFilter_UPSTREAM_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_GATEWAY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
					Cluster: &networking.EnvoyFilter_ClusterMatch{
						Name: "cluster2",
						Filter: &networking.EnvoyFilter_ClusterMatch_UpstreamFilterMatch{
							Name: "http-filter3",
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_INSERT_AFTER,
				Value:     buildPatchStruct(`{"name":"http-filter4"}`),
			},
		},
		// Insert first upstream http filter
		{
			ApplyTo: networking.EnvoyFilter_UPSTREAM_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_GATEWAY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
					Cluster: &networking.EnvoyFilter_ClusterMatch{
						Name: "cluster2",
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_INSERT_FIRST,
				Value:     buildPatchStruct(`{"name":"http-filter5"}`),
			},
		},
		// Insert upstream http filter before existing filter without a filter match
		{
			ApplyTo: networking.EnvoyFilter_UPSTREAM_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_GATEWAY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
					Cluster: &networking.EnvoyFilter_ClusterMatch{
						Name: "cluster2",
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_INSERT_BEFORE,
				Value:     buildPatchStruct(`{"name":"http-filter6"}`),
			},
		},
		// Insert upstream http filter after without a filter match
		{
			ApplyTo: networking.EnvoyFilter_UPSTREAM_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_GATEWAY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
					Cluster: &networking.EnvoyFilter_ClusterMatch{
						Name: "cluster2",
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_INSERT_AFTER,
				Value:     buildPatchStruct(`{"name":"http-filter7"}`),
			},
		},
		// Replace upstream http filter
		{
			ApplyTo: networking.EnvoyFilter_UPSTREAM_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_GATEWAY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
					Cluster: &networking.EnvoyFilter_ClusterMatch{
						Name: "cluster2",
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name":"http-filter-to-be-replaced"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_UPSTREAM_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_GATEWAY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
					Cluster: &networking.EnvoyFilter_ClusterMatch{
						Filter: &networking.EnvoyFilter_ClusterMatch_UpstreamFilterMatch{
							Name: "http-filter-to-be-replaced",
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REPLACE,
				Value:     buildPatchStruct(`{"name":"http-filter8"}`),
			},
		},
		// Merge upstream http filter
		{
			ApplyTo: networking.EnvoyFilter_UPSTREAM_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_GATEWAY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
					Cluster: &networking.EnvoyFilter_ClusterMatch{
						Name: "cluster2",
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value: buildPatchStruct(`
{"name":"envoy.filters.http.admission_control",
"typed_config":{
	"@type":"type.googleapis.com/envoy.extensions.filters.http.admission_control.v3.AdmissionControl",
	"enabled": {
		"default_value": false
	},
	"success_criteria":{}
}}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_UPSTREAM_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_GATEWAY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
					Cluster: &networking.EnvoyFilter_ClusterMatch{
						Name: "cluster2",
						Filter: &networking.EnvoyFilter_ClusterMatch_UpstreamFilterMatch{
							Name: "envoy.filters.http.admission_control",
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value: buildPatchStruct(`
{"name":"envoy.filters.http.admission_control",
"typed_config":{
	"@type":"type.googleapis.com/envoy.extensions.filters.http.admission_control.v3.AdmissionControl",
	"enabled": {
		"default_value": true
	}
}
}`),
			},
		},
	}

	sidecarOutboundIn := []*cluster.Cluster{
		{
			Name:            "outbound|443||cluster1",
			DnsLookupFamily: cluster.Cluster_V4_ONLY,
			LbPolicy:        cluster.Cluster_ROUND_ROBIN,
			TransportSocketMatches: []*cluster.Cluster_TransportSocketMatch{
				{
					Name: "tlsMode-istio",
					TransportSocket: &core.TransportSocket{
						Name: "envoy.transport_sockets.tls",
						ConfigType: &core.TransportSocket_TypedConfig{
							TypedConfig: protoconv.MessageToAny(&tls.UpstreamTlsContext{
								CommonTlsContext: &tls.CommonTlsContext{
									TlsParams: &tls.TlsParameters{
										EcdhCurves:                []string{"X25519"},
										TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_1,
									},
									TlsCertificateCertificateProviderInstance: &tls.CommonTlsContext_CertificateProviderInstance{
										InstanceName:    "instance",
										CertificateName: "certificate",
									},
								},
							}),
						},
					},
				},
			},
		},
		{
			Name: "outbound|443||cluster2",
			Http2ProtocolOptions: &core.Http2ProtocolOptions{
				AllowConnect:  true,
				AllowMetadata: true,
			}, LbPolicy: cluster.Cluster_MAGLEV,
		},
		{
			Name:            "outbound|443||cluster3",
			DnsLookupFamily: cluster.Cluster_V4_ONLY,
			LbPolicy:        cluster.Cluster_ROUND_ROBIN,
			TransportSocket: &core.TransportSocket{
				Name: "envoy.transport_sockets.tls",
				ConfigType: &core.TransportSocket_TypedConfig{
					TypedConfig: protoconv.MessageToAny(&tls.UpstreamTlsContext{
						CommonTlsContext: &tls.CommonTlsContext{
							TlsParams: &tls.TlsParameters{
								EcdhCurves:                []string{"X25519"},
								TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							},
							TlsCertificateCertificateProviderInstance: &tls.CommonTlsContext_CertificateProviderInstance{
								InstanceName:    "instance",
								CertificateName: "certificate",
							},
						},
					}),
				},
			},
		},
		{
			Name: "outbound|7777||custom-tls-addition",
		},
		{
			Name:            "outbound|7777||custom-tls-replacement",
			DnsLookupFamily: cluster.Cluster_V4_ONLY,
			LbPolicy:        cluster.Cluster_ROUND_ROBIN,
			TransportSocket: &core.TransportSocket{
				Name: "envoy.transport_sockets.tls",
				ConfigType: &core.TransportSocket_TypedConfig{
					TypedConfig: protoconv.MessageToAny(&tls.UpstreamTlsContext{
						CommonTlsContext: &tls.CommonTlsContext{
							TlsParams: &tls.TlsParameters{
								EcdhCurves:                []string{"X25519"},
								TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_1,
							},
							TlsCertificateCertificateProviderInstance: &tls.CommonTlsContext_CertificateProviderInstance{
								InstanceName:    "instance",
								CertificateName: "certificate",
							},
						},
					}),
				},
			},
		},
		{
			Name:            "outbound|7777||custom-tls-replacement-tsm",
			DnsLookupFamily: cluster.Cluster_V4_ONLY,
			LbPolicy:        cluster.Cluster_ROUND_ROBIN,
			TransportSocketMatches: []*cluster.Cluster_TransportSocketMatch{
				{
					Name: "tlsMode-istio",
					TransportSocket: &core.TransportSocket{
						Name: "envoy.transport_sockets.tls",
						ConfigType: &core.TransportSocket_TypedConfig{
							TypedConfig: protoconv.MessageToAny(&tls.UpstreamTlsContext{
								CommonTlsContext: &tls.CommonTlsContext{
									TlsParams: &tls.TlsParameters{
										EcdhCurves:                []string{"X25519"},
										TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_1,
									},
									TlsCertificateCertificateProviderInstance: &tls.CommonTlsContext_CertificateProviderInstance{
										InstanceName:    "instance",
										CertificateName: "certificate",
									},
								},
							}),
						},
					},
				},
			},
		},
	}

	sidecarOutboundOut := []*cluster.Cluster{
		{
			Name:            "outbound|443||cluster1",
			DnsLookupFamily: cluster.Cluster_V6_ONLY,
			LbPolicy:        cluster.Cluster_RING_HASH,
			TransportSocketMatches: []*cluster.Cluster_TransportSocketMatch{
				{
					Name: "tlsMode-istio",
					TransportSocket: &core.TransportSocket{
						Name: "envoy.transport_sockets.tls",
						ConfigType: &core.TransportSocket_TypedConfig{
							TypedConfig: protoconv.MessageToAny(&tls.UpstreamTlsContext{
								CommonTlsContext: &tls.CommonTlsContext{
									TlsParams: &tls.TlsParameters{
										EcdhCurves:                []string{"X25519"},
										TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_3,
									},
									TlsCertificateCertificateProviderInstance: &tls.CommonTlsContext_CertificateProviderInstance{
										InstanceName:    "instance",
										CertificateName: "certificate",
									},
								},
							}),
						},
					},
				},
			},
		},
		{
			Name: "outbound|443||cluster2",
			Http2ProtocolOptions: &core.Http2ProtocolOptions{
				AllowConnect:  true,
				AllowMetadata: true,
			},
			LbPolicy:        cluster.Cluster_RING_HASH,
			DnsLookupFamily: cluster.Cluster_V6_ONLY,
			TransportSocket: &core.TransportSocket{
				Name: "envoy.transport_sockets.tls",
				ConfigType: &core.TransportSocket_TypedConfig{
					TypedConfig: protoconv.MessageToAny(&tls.UpstreamTlsContext{
						CommonTlsContext: &tls.CommonTlsContext{
							TlsParams: &tls.TlsParameters{
								TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_3,
							},
						},
					}),
				},
			},
		},
		{
			Name:            "outbound|443||cluster3",
			DnsLookupFamily: cluster.Cluster_V6_ONLY,
			LbPolicy:        cluster.Cluster_RING_HASH,
			TransportSocket: &core.TransportSocket{
				Name: "envoy.transport_sockets.tls",
				ConfigType: &core.TransportSocket_TypedConfig{
					TypedConfig: protoconv.MessageToAny(&tls.UpstreamTlsContext{
						CommonTlsContext: &tls.CommonTlsContext{
							TlsParams: &tls.TlsParameters{
								EcdhCurves:                []string{"X25519"},
								TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_3,
								TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
							},
							TlsCertificateCertificateProviderInstance: &tls.CommonTlsContext_CertificateProviderInstance{
								InstanceName:    "instance",
								CertificateName: "certificate",
							},
						},
					}),
				},
			},
		},
		{
			Name:            "outbound|7777||custom-tls-addition",
			DnsLookupFamily: cluster.Cluster_V6_ONLY,
			LbPolicy:        cluster.Cluster_RING_HASH,
			TransportSocket: &core.TransportSocket{
				Name: "transport_sockets.alts",
				ConfigType: &core.TransportSocket_TypedConfig{
					TypedConfig: protoconv.MessageToAny(&udpa.TypedStruct{
						TypeUrl: "type.googleapis.com/envoy.extensions.transport_sockets.alts.v3.Alts",
						Value:   buildGolangPatchStruct(`{"handshaker_service":"1.2.3.4"}`),
					}),
				},
			},
		},
		{
			Name:            "outbound|7777||custom-tls-replacement",
			DnsLookupFamily: cluster.Cluster_V6_ONLY,
			LbPolicy:        cluster.Cluster_RING_HASH,
			TransportSocket: &core.TransportSocket{
				Name: "transport_sockets.alts",
				ConfigType: &core.TransportSocket_TypedConfig{
					TypedConfig: protoconv.MessageToAny(&udpa.TypedStruct{
						TypeUrl: "type.googleapis.com/envoy.extensions.transport_sockets.alts.v3.Alts",
						Value:   buildGolangPatchStruct(`{"handshaker_service":"1.2.3.4"}`),
					}),
				},
			},
		},
		{
			Name:            "outbound|7777||custom-tls-replacement-tsm",
			DnsLookupFamily: cluster.Cluster_V6_ONLY,
			LbPolicy:        cluster.Cluster_RING_HASH,
			TransportSocketMatches: []*cluster.Cluster_TransportSocketMatch{
				{
					Name: "tlsMode-istio",
					TransportSocket: &core.TransportSocket{
						Name: "envoy.transport_sockets.tls",
						ConfigType: &core.TransportSocket_TypedConfig{
							TypedConfig: protoconv.MessageToAny(&tls.UpstreamTlsContext{
								CommonTlsContext: &tls.CommonTlsContext{
									TlsParams: &tls.TlsParameters{
										EcdhCurves:                []string{"X25519"},
										TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_1,
									},
									TlsCertificateCertificateProviderInstance: &tls.CommonTlsContext_CertificateProviderInstance{
										InstanceName:    "instance",
										CertificateName: "certificate",
									},
								},
							}),
						},
					},
				},
			},
		},
		{Name: "new-cluster1"},
		{Name: "new-cluster2"},
	}

	sidecarInboundIn := []*cluster.Cluster{
		{Name: "cluster1", DnsLookupFamily: cluster.Cluster_V4_ONLY, LbPolicy: cluster.Cluster_ROUND_ROBIN},
		{Name: "inbound|9999||mgmtCluster"},
	}
	sidecarInboundOut := []*cluster.Cluster{
		{Name: "cluster1", DnsLookupFamily: cluster.Cluster_V6_ONLY, LbPolicy: cluster.Cluster_RING_HASH},
	}

	sidecarInboundServiceIn := []*cluster.Cluster{
		{Name: "inbound|7443||", DnsLookupFamily: cluster.Cluster_V4_ONLY, LbPolicy: cluster.Cluster_ROUND_ROBIN},
	}
	sidecarInboundServiceOut := []*cluster.Cluster{
		{
			Name: "inbound|7443||", ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
			DnsLookupFamily: cluster.Cluster_V6_ONLY, LbPolicy: cluster.Cluster_RING_HASH,
		},
	}

	gatewayInput := []*cluster.Cluster{
		{Name: "cluster1", DnsLookupFamily: cluster.Cluster_V4_ONLY, LbPolicy: cluster.Cluster_ROUND_ROBIN},
		{
			Name: "cluster2",
			TypedExtensionProtocolOptions: map[string]*anypb.Any{
				v3.HttpProtocolOptionsType: protoconv.MessageToAny(&http.HttpProtocolOptions{
					UpstreamProtocolOptions: &http.HttpProtocolOptions_ExplicitHttpConfig_{
						ExplicitHttpConfig: &http.HttpProtocolOptions_ExplicitHttpConfig{
							ProtocolConfig: &http.HttpProtocolOptions_ExplicitHttpConfig_Http2ProtocolOptions{
								Http2ProtocolOptions: &core.Http2ProtocolOptions{
									AllowConnect:  true,
									AllowMetadata: true,
								},
							},
						},
					},
				}),
			},
			LbPolicy: cluster.Cluster_MAGLEV,
		},
		{Name: "outbound|443||gateway.com"},
	}
	gatewayOutput := []*cluster.Cluster{
		{Name: "cluster1", DnsLookupFamily: cluster.Cluster_V6_ONLY, LbPolicy: cluster.Cluster_RING_HASH},
		{
			Name: "cluster2",
			TypedExtensionProtocolOptions: map[string]*anypb.Any{
				v3.HttpProtocolOptionsType: protoconv.MessageToAny(&http.HttpProtocolOptions{
					UpstreamProtocolOptions: &http.HttpProtocolOptions_ExplicitHttpConfig_{
						ExplicitHttpConfig: &http.HttpProtocolOptions_ExplicitHttpConfig{
							ProtocolConfig: &http.HttpProtocolOptions_ExplicitHttpConfig_Http2ProtocolOptions{
								Http2ProtocolOptions: &core.Http2ProtocolOptions{
									AllowConnect:  true,
									AllowMetadata: true,
								},
							},
						},
					},
					HttpFilters: []*hcm.HttpFilter{
						{
							Name: "http-filter6",
						},
						{
							Name: "http-filter5",
						},
						{
							Name: "http-filter1",
						},
						{
							Name: "http-filter3",
						},
						{
							Name: "http-filter4",
						},
						{
							Name: "http-filter2",
						},
						{
							Name: "http-filter7",
						},
						{
							Name: "http-filter8",
						},
						{
							Name: "envoy.filters.http.admission_control",
							ConfigType: &hcm.HttpFilter_TypedConfig{
								TypedConfig: protoconv.MessageToAny(&admission.AdmissionControl{
									Enabled: &core.RuntimeFeatureFlag{
										DefaultValue: wrapperspb.Bool(true),
									},
									EvaluationCriteria: &admission.AdmissionControl_SuccessCriteria_{
										SuccessCriteria: &admission.AdmissionControl_SuccessCriteria{},
									},
								}),
							},
						},
					},
				}),
			}, LbPolicy: cluster.Cluster_RING_HASH, DnsLookupFamily: cluster.Cluster_V6_ONLY,
		},
	}

	testCases := []struct {
		name         string
		input        []*cluster.Cluster
		proxy        *model.Proxy
		host         string
		patchContext networking.EnvoyFilter_PatchContext
		output       []*cluster.Cluster
	}{
		{
			name:         "sidecar outbound cluster patch",
			input:        sidecarOutboundIn,
			proxy:        &model.Proxy{Type: model.SidecarProxy, ConfigNamespace: "not-default"},
			patchContext: networking.EnvoyFilter_SIDECAR_OUTBOUND,
			output:       sidecarOutboundOut,
		},
		{
			name:         "sidecar inbound cluster patch",
			input:        sidecarInboundIn,
			patchContext: networking.EnvoyFilter_SIDECAR_INBOUND,
			proxy:        &model.Proxy{Type: model.SidecarProxy, ConfigNamespace: "not-default"},
			output:       sidecarInboundOut,
		},
		{
			name:         "sidecar inbound cluster patch with service name",
			input:        sidecarInboundServiceIn,
			patchContext: networking.EnvoyFilter_SIDECAR_INBOUND,
			proxy:        &model.Proxy{Type: model.SidecarProxy, ConfigNamespace: "not-default"},
			host:         "service.servicens",
			output:       sidecarInboundServiceOut,
		},
		{
			name:         "gateway cds patch",
			input:        gatewayInput,
			patchContext: networking.EnvoyFilter_GATEWAY,
			proxy:        &model.Proxy{Type: model.Router, ConfigNamespace: "not-default"},
			output:       gatewayOutput,
		},
	}

	serviceDiscovery := memory.NewServiceDiscovery()
	env := newTestEnvironment(serviceDiscovery, testMesh, buildEnvoyFilterConfigStore(configPatches))
	push := model.NewPushContext()
	push.InitContext(env, nil, nil)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			efw := push.EnvoyFilters(tc.proxy)
			output := []*cluster.Cluster{}
			for _, c := range tc.input {
				if pc := ApplyClusterMerge(tc.patchContext, efw, c, []host.Name{host.Name(tc.host)}); pc != nil {
					output = append(output, pc)
				}
			}
			output = append(output, InsertedClusters(tc.patchContext, efw)...)
			if diff := cmp.Diff(tc.output, output, protocmp.Transform()); diff != "" {
				t.Errorf("%s mismatch (-want +got):\n%s", tc.name, diff)
			}
		})
	}
}
