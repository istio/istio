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

package filters

import (
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	sfsvalue "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/common/set_filter_state/v3"
	cors "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/cors/v3"
	fault "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/fault/v3"
	grpcstats "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/grpc_stats/v3"
	grpcweb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/grpc_web/v3"
	router "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	sfs "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/set_filter_state/v3"
	statefulsession "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/stateful_session/v3"
	httpinspector "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/http_inspector/v3"
	originaldst "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/original_dst/v3"
	originalsrc "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/original_src/v3"
	proxy_proto "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/proxy_protocol/v3"
	tlsinspector "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/tls_inspector/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	sfsnetwork "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/set_filter_state/v3"
	previoushost "github.com/envoyproxy/go-control-plane/envoy/extensions/retry/host/previous_hosts/v3"
	resourcedetectors "github.com/envoyproxy/go-control-plane/envoy/extensions/tracers/opentelemetry/resource_detectors/v3"
	rawbuffer "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/raw_buffer/v3"
	"google.golang.org/protobuf/types/known/wrapperspb"

	alpn "istio.io/api/envoy/config/filter/http/alpn/v2alpha1"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/wellknown"
)

const (
	TLSTransportProtocol       = "tls"
	RawBufferTransportProtocol = "raw_buffer"

	// Alpn HTTP filter name which will override the ALPN for upstream TLS connection.
	AlpnFilterName = "istio.alpn"

	MxFilterName = "istio.metadata_exchange"

	// AuthnFilterName is the name for the Istio AuthN filter. This should be the same
	// as the name defined in
	// https://github.com/istio/proxy/blob/master/src/envoy/http/authn/http_filter_factory.cc#L30
	AuthnFilterName = "istio_authn"

	// EnvoyJwtFilterName is the name of the Envoy JWT filter.
	EnvoyJwtFilterName = "envoy.filters.http.jwt_authn"

	// EnvoyJwtFilterPayload is the struct field for the payload in dynamic metadata in Envoy JWT filter.
	EnvoyJwtFilterPayload = "payload"
)

// Define static filters to be reused across the codebase. This avoids duplicate marshaling/unmarshaling
// This should not be used for filters that will be mutated
var (
	RetryPreviousHosts = &route.RetryPolicy_RetryHostPredicate{
		Name: "envoy.retry_host_predicates.previous_hosts",
		ConfigType: &route.RetryPolicy_RetryHostPredicate_TypedConfig{
			TypedConfig: protoconv.MessageToAny(&previoushost.PreviousHostsPredicate{}),
		},
	}
	RawBufferTransportSocket = &core.TransportSocket{
		Name: wellknown.TransportSocketRawBuffer,
		ConfigType: &core.TransportSocket_TypedConfig{
			TypedConfig: protoconv.MessageToAny(&rawbuffer.RawBuffer{}),
		},
	}
	Cors = &hcm.HttpFilter{
		Name: wellknown.CORS,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(&cors.Cors{}),
		},
	}
	Fault = &hcm.HttpFilter{
		Name: wellknown.Fault,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(&fault.HTTPFault{}),
		},
	}
	GrpcWeb = &hcm.HttpFilter{
		Name: wellknown.GRPCWeb,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(&grpcweb.GrpcWeb{}),
		},
	}
	GrpcStats = &hcm.HttpFilter{
		Name: wellknown.HTTPGRPCStats,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(&grpcstats.FilterConfig{
				EmitFilterState: true,
				PerMethodStatSpecifier: &grpcstats.FilterConfig_StatsForAllMethods{
					StatsForAllMethods: &wrapperspb.BoolValue{Value: false},
				},
			}),
		},
	}
	TLSInspector = &listener.ListenerFilter{
		Name: wellknown.TLSInspector,
		ConfigType: &listener.ListenerFilter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(&tlsinspector.TlsInspector{}),
		},
	}
	HTTPInspector = &listener.ListenerFilter{
		Name: wellknown.HTTPInspector,
		ConfigType: &listener.ListenerFilter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(&httpinspector.HttpInspector{}),
		},
	}
	OriginalDestination = &listener.ListenerFilter{
		Name: wellknown.OriginalDestination,
		ConfigType: &listener.ListenerFilter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(&originaldst.OriginalDst{}),
		},
	}
	OriginalSrc = &listener.ListenerFilter{
		Name: wellknown.OriginalSource,
		ConfigType: &listener.ListenerFilter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(&originalsrc.OriginalSrc{
				Mark: 1337,
			}),
		},
	}
	ProxyProtocol = &listener.ListenerFilter{
		Name: wellknown.ProxyProtocol,
		ConfigType: &listener.ListenerFilter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(&proxy_proto.ProxyProtocol{}),
		},
	}
	EmptySessionFilter = &hcm.HttpFilter{
		Name: util.StatefulSessionFilter,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(&statefulsession.StatefulSession{}),
		},
	}
	Alpn = &hcm.HttpFilter{
		Name: AlpnFilterName,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(&alpn.FilterConfig{
				AlpnOverride: []*alpn.FilterConfig_AlpnOverride{
					{
						UpstreamProtocol: alpn.FilterConfig_HTTP10,
						AlpnOverride:     mtlsHTTP10ALPN,
					},
					{
						UpstreamProtocol: alpn.FilterConfig_HTTP11,
						AlpnOverride:     mtlsHTTP11ALPN,
					},
					{
						UpstreamProtocol: alpn.FilterConfig_HTTP2,
						AlpnOverride:     mtlsHTTP2ALPN,
					},
				},
			}),
		},
	}

	// TCP MX is an Istio filter defined in https://github.com/istio/proxy/tree/master/source/extensions/filters/network/metadata_exchange.
	tcpMx = protoconv.TypedStructWithFields("type.googleapis.com/envoy.tcp.metadataexchange.config.MetadataExchange",
		map[string]any{
			"protocol":         "istio-peer-exchange",
			"enable_discovery": true,
		})

	TCPListenerMx = &listener.Filter{
		Name:       MxFilterName,
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: tcpMx},
	}

	TCPClusterMx = &cluster.Filter{
		Name:        MxFilterName,
		TypedConfig: tcpMx,
	}

	WaypointDownstreamMetadataFilter = &hcm.HttpFilter{
		Name: "waypoint_downstream_peer_metadata",
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: protoconv.TypedStructWithFields("type.googleapis.com/io.istio.http.peer_metadata.Config",
				map[string]any{
					"downstream_discovery": []any{
						map[string]any{
							"workload_discovery": map[string]any{},
						},
					},
					"shared_with_upstream": true,
				}),
		},
	}

	WaypointUpstreamMetadataFilter = &hcm.HttpFilter{
		Name: "waypoint_upstream_peer_metadata",
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: protoconv.TypedStructWithFields("type.googleapis.com/io.istio.http.peer_metadata.Config",
				map[string]any{
					"upstream_discovery": []any{
						map[string]any{
							"workload_discovery": map[string]any{},
						},
					},
				}),
		},
	}

	SidecarInboundMetadataFilter = &hcm.HttpFilter{
		Name: MxFilterName,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: protoconv.TypedStructWithFields("type.googleapis.com/io.istio.http.peer_metadata.Config",
				map[string]any{
					"downstream_discovery": []any{
						map[string]any{
							"istio_headers": map[string]any{},
						},
						map[string]any{
							"workload_discovery": map[string]any{},
						},
					},
					"downstream_propagation": []any{
						map[string]any{
							"istio_headers": map[string]any{},
						},
					},
				}),
		},
	}

	SidecarOutboundMetadataFilter = &hcm.HttpFilter{
		Name: MxFilterName,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: protoconv.TypedStructWithFields("type.googleapis.com/io.istio.http.peer_metadata.Config",
				map[string]any{
					"upstream_discovery": []any{
						map[string]any{
							"istio_headers": map[string]any{},
						},
						map[string]any{
							"workload_discovery": map[string]any{},
						},
					},
					"upstream_propagation": []any{
						map[string]any{
							"istio_headers": map[string]any{},
						},
					},
				}),
		},
	}
	// TODO https://github.com/istio/istio/issues/46740
	// false values can be omitted in protobuf, results in diff JSON values between control plane and envoy config dumps
	// long term fix will be to add the metadata config to istio/api and use that over TypedStruct
	SidecarOutboundMetadataFilterSkipHeaders = &hcm.HttpFilter{
		Name: MxFilterName,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: protoconv.TypedStructWithFields("type.googleapis.com/io.istio.http.peer_metadata.Config",
				map[string]any{
					"upstream_discovery": []any{
						map[string]any{
							"istio_headers": map[string]any{},
						},
						map[string]any{
							"workload_discovery": map[string]any{},
						},
					},
					"upstream_propagation": []any{
						map[string]any{
							"istio_headers": map[string]any{
								"skip_external_clusters": true,
							},
						},
					},
				}),
		},
	}

	ConnectAuthorityFilter = &hcm.HttpFilter{
		Name: "connect_authority",
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(&sfs.Config{
				OnRequestHeaders: []*sfsvalue.FilterStateValue{
					{
						Key: &sfsvalue.FilterStateValue_ObjectKey{
							ObjectKey: "envoy.filters.listener.original_dst.local_ip",
						},
						Value: &sfsvalue.FilterStateValue_FormatString{
							FormatString: &core.SubstitutionFormatString{
								Format: &core.SubstitutionFormatString_TextFormatSource{
									TextFormatSource: &core.DataSource{
										Specifier: &core.DataSource_InlineString{
											InlineString: "%REQ(:AUTHORITY)%",
										},
									},
								},
							},
						},
						SharedWithUpstream: sfsvalue.FilterStateValue_ONCE,
					}, {
						Key: &sfsvalue.FilterStateValue_ObjectKey{
							ObjectKey: "envoy.filters.listener.original_dst.remote_ip",
						},
						Value: &sfsvalue.FilterStateValue_FormatString{
							FormatString: &core.SubstitutionFormatString{
								Format: &core.SubstitutionFormatString_TextFormatSource{
									TextFormatSource: &core.DataSource{
										Specifier: &core.DataSource_InlineString{
											InlineString: "%DOWNSTREAM_REMOTE_ADDRESS%",
										},
									},
								},
							},
						},
						SharedWithUpstream: sfsvalue.FilterStateValue_ONCE,
					}, {
						Key: &sfsvalue.FilterStateValue_ObjectKey{
							ObjectKey: "io.istio.peer_principal",
						},
						FactoryKey: "envoy.string",
						Value: &sfsvalue.FilterStateValue_FormatString{
							FormatString: &core.SubstitutionFormatString{
								Format: &core.SubstitutionFormatString_TextFormatSource{
									TextFormatSource: &core.DataSource{
										Specifier: &core.DataSource_InlineString{
											InlineString: "%DOWNSTREAM_PEER_URI_SAN%",
										},
									},
								},
							},
						},
						SharedWithUpstream: sfsvalue.FilterStateValue_ONCE,
					}, {
						Key: &sfsvalue.FilterStateValue_ObjectKey{
							ObjectKey: "io.istio.local_principal",
						},
						FactoryKey: "envoy.string",
						Value: &sfsvalue.FilterStateValue_FormatString{
							FormatString: &core.SubstitutionFormatString{
								Format: &core.SubstitutionFormatString_TextFormatSource{
									TextFormatSource: &core.DataSource{
										Specifier: &core.DataSource_InlineString{
											InlineString: "%DOWNSTREAM_LOCAL_URI_SAN%",
										},
									},
								},
							},
						},
						SharedWithUpstream: sfsvalue.FilterStateValue_ONCE,
					},
				},
			}),
		},
	}

	ConnectAuthorityNetworkFilter = &listener.Filter{
		Name: "connect_authority",
		ConfigType: &listener.Filter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(&sfsnetwork.Config{
				OnNewConnection: []*sfsvalue.FilterStateValue{{
					Key: &sfsvalue.FilterStateValue_ObjectKey{
						ObjectKey: "envoy.filters.listener.original_dst.local_ip",
					},
					Value: &sfsvalue.FilterStateValue_FormatString{
						FormatString: &core.SubstitutionFormatString{
							Format: &core.SubstitutionFormatString_TextFormatSource{
								TextFormatSource: &core.DataSource{
									Specifier: &core.DataSource_InlineString{
										InlineString: "%FILTER_STATE(envoy.filters.listener.original_dst.local_ip:PLAIN)%",
									},
								},
							},
						},
					},
					SharedWithUpstream: sfsvalue.FilterStateValue_ONCE,
				}},
			}),
		},
	}
)

// Router is used a bunch, so its worth precomputing even though we have a few options.
// Since there are only 4 possible options, just precompute them all
var routers = func() map[RouterFilterContext]*hcm.HttpFilter {
	res := map[RouterFilterContext]*hcm.HttpFilter{}
	for _, startSpan := range []bool{true, false} {
		for _, suppressHeaders := range []bool{true, false} {
			res[RouterFilterContext{
				StartChildSpan:       startSpan,
				SuppressDebugHeaders: suppressHeaders,
			}] = &hcm.HttpFilter{
				Name: wellknown.Router,
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: protoconv.MessageToAny(&router.Router{
						StartChildSpan:       startSpan,
						SuppressEnvoyHeaders: suppressHeaders,
					}),
				},
			}
		}
	}
	return res
}()

func BuildRouterFilter(ctx RouterFilterContext) *hcm.HttpFilter {
	return routers[ctx]
}

var (
	// These ALPNs are injected in the client side by the ALPN filter.
	// "istio" is added for each upstream protocol in order to make it
	// backward compatible. e.g., 1.4 proxy -> 1.3 proxy.
	// Non istio-* variants are added to ensure that traffic sent out of the mesh has a valid ALPN;
	// ideally this would not be added, but because the override filter is in the HCM, rather than cluster,
	// we do not yet know the upstream so we cannot determine if its in or out of the mesh
	mtlsHTTP10ALPN = []string{"istio-http/1.0", "istio", "http/1.0"}
	mtlsHTTP11ALPN = []string{"istio-http/1.1", "istio", "http/1.1"}
	mtlsHTTP2ALPN  = []string{"istio-h2", "istio", "h2"}
)

// OpenTelemetry Resource Detectors
var (
	EnvironmentResourceDetector = &core.TypedExtensionConfig{
		Name:        "envoy.tracers.opentelemetry.resource_detectors.environment",
		TypedConfig: protoconv.MessageToAny(&resourcedetectors.EnvironmentResourceDetectorConfig{}),
	}
	DynatraceResourceDetector = &core.TypedExtensionConfig{
		Name:        "envoy.tracers.opentelemetry.resource_detectors.dynatrace",
		TypedConfig: protoconv.MessageToAny(&resourcedetectors.DynatraceResourceDetectorConfig{}),
	}
)
