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
	extproc "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	fault "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/fault/v3"
	grpcstats "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/grpc_stats/v3"
	grpcweb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/grpc_web/v3"
	router "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	sfs "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/set_filter_state/v3"
	statefulsession "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/stateful_session/v3"
	upstreamcodec "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/upstream_codec/v3"
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
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	alpn "istio.io/api/envoy/config/filter/http/alpn/v2alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/wellknown"
)

const (
	TLSTransportProtocol       = "tls"
	RawBufferTransportProtocol = "raw_buffer"

	// Alpn HTTP filter name which will override the ALPN for upstream TLS connection.
	AlpnFilterName = "istio.alpn"
	// MxFilterName TCP MX is an Istio filter defined in https://github.com/istio/proxy/tree/master/source/extensions/filters/network/metadata_exchange.
	MxFilterName = "istio.metadata_exchange"

	// EnvoyJwtFilterName is the name of the Envoy JWT filter.
	EnvoyJwtFilterName = "envoy.filters.http.jwt_authn"

	// EnvoyJwtFilterPayload is the struct field for the payload in dynamic metadata in Envoy JWT filter.
	EnvoyJwtFilterPayload = "payload"

	PeerMetadataTypeURL     = "type.googleapis.com/io.istio.http.peer_metadata.Config"
	MetadataExchangeTypeURL = "type.googleapis.com/envoy.tcp.metadataexchange.config.MetadataExchange"
	// OriginalDstFilterStateKey is a filter state key where we store the :authority. This has traditionally been an
	// IP address, but it can also be a hostname if the incoming CONNECT tunnel was sent via double-HBONE.
	// It will fail if the value is not a valid IP address.
	OriginalDstFilterStateKey = "envoy.filters.listener.original_dst.local_ip"

	// Authority Key is another filter state key where we store :authority. Because this is not a
	// well-known filter state key, we can store non-IP address :authorities in here
	AuthorityFilterStateKey = "io.istio.connect_authority"
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
			TypedConfig: protoconv.MessageToAny(&tlsinspector.TlsInspector{
				InitialReadBufferSize: &wrapperspb.UInt32Value{Value: 512}, // Default is 64KB.
			}),
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

	WaypointDownstreamMetadataFilter = &hcm.HttpFilter{
		Name: "waypoint_downstream_peer_metadata",
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: protoconv.TypedStructWithFields(PeerMetadataTypeURL,
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
			TypedConfig: protoconv.TypedStructWithFields(PeerMetadataTypeURL,
				map[string]any{
					"upstream_discovery": []any{
						map[string]any{
							"workload_discovery": map[string]any{},
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
							ObjectKey: OriginalDstFilterStateKey,
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
							ObjectKey: AuthorityFilterStateKey,
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
						FactoryKey:         "envoy.string",
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
						ObjectKey: OriginalDstFilterStateKey,
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

	UpstreamCodecFilter = &hcm.HttpFilter{
		Name: wellknown.HTTPUpstreamCodec,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(&upstreamcodec.UpstreamCodec{}),
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

func BuildInferencePoolExtProcFilter(extProcCluster string, failureModeAllow bool) *hcm.HttpFilter {
	return &hcm.HttpFilter{
		Name: wellknown.HTTPExternalProcessing,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: protoconv.MessageToAny(&extproc.ExternalProcessor{
				GrpcService: &core.GrpcService{
					TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
						EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
							ClusterName: extProcCluster,
						},
					},
					Timeout: &durationpb.Duration{Seconds: 10},
				},
				FailureModeAllow: failureModeAllow,
				ProcessingMode: &extproc.ProcessingMode{
					RequestHeaderMode: extproc.ProcessingMode_SEND,
					// open AI standard includes the model and other information the ext_proc server needs in the request body
					RequestBodyMode: extproc.ProcessingMode_FULL_DUPLEX_STREAMED,
					// If the ext_proc server has the request_body_mode set to FULL_DUPLEX_STREAMED, then the request_trailer_mode has to be set to SEND
					RequestTrailerMode: extproc.ProcessingMode_SEND,
					ResponseHeaderMode: extproc.ProcessingMode_SEND,
					// GIE collects statistics present in the open AI standard response message
					ResponseBodyMode: extproc.ProcessingMode_FULL_DUPLEX_STREAMED,
					// If the ext_proc server has the response_body_mode set to FULL_DUPLEX_STREAMED, then the response_trailer_mode has to be set to SEND
					ResponseTrailerMode: extproc.ProcessingMode_SEND,
				},
				MessageTimeout: &durationpb.Duration{Seconds: 1000},
				MetadataOptions: &extproc.MetadataOptions{
					ReceivingNamespaces: &extproc.MetadataOptions_MetadataNamespaces{
						Untyped: []string{constants.EnvoySubsetNamespace},
					},
					ForwardingNamespaces: &extproc.MetadataOptions_MetadataNamespaces{
						Untyped: []string{constants.EnvoySubsetNamespace},
					},
				},
			}),
		},
	}
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

var (
	TCPClusterMx = func() *cluster.Filter {
		cfg := map[string]any{
			"protocol":         "istio-peer-exchange",
			"enable_discovery": true,
		}
		additionalLabels(cfg)

		return &cluster.Filter{
			Name:        MxFilterName,
			TypedConfig: protoconv.TypedStructWithFields(MetadataExchangeTypeURL, cfg),
		}
	}()

	TCPListenerMx = func() *listener.Filter {
		cfg := map[string]any{
			"protocol":         "istio-peer-exchange",
			"enable_discovery": true,
		}
		additionalLabels(cfg)

		return &listener.Filter{
			Name: MxFilterName,
			ConfigType: &listener.Filter_TypedConfig{
				TypedConfig: protoconv.TypedStructWithFields(MetadataExchangeTypeURL, cfg),
			},
		}
	}()

	SidecarInboundMetadataFilter = func() *hcm.HttpFilter {
		cfg := map[string]any{
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
		}
		additionalLabels(cfg)

		return &hcm.HttpFilter{
			Name: MxFilterName,
			ConfigType: &hcm.HttpFilter_TypedConfig{
				TypedConfig: protoconv.TypedStructWithFields(PeerMetadataTypeURL, cfg),
			},
		}
	}()

	SidecarOutboundMetadataFilter = func() *hcm.HttpFilter {
		cfg := map[string]any{
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
		}
		additionalLabels(cfg)

		return &hcm.HttpFilter{
			Name: MxFilterName,
			ConfigType: &hcm.HttpFilter_TypedConfig{
				TypedConfig: protoconv.TypedStructWithFields(PeerMetadataTypeURL, cfg),
			},
		}
	}()

	SidecarOutboundMetadataFilterSkipHeaders = func() *hcm.HttpFilter {
		// TODO https://github.com/istio/istio/issues/46740
		// false values can be omitted in protobuf, results in diff JSON values between control plane and envoy config dumps
		// long term fix will be to add the metadata config to istio/api and use that over TypedStruct
		cfg := map[string]any{
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
		}
		additionalLabels(cfg)

		return &hcm.HttpFilter{
			Name: MxFilterName,
			ConfigType: &hcm.HttpFilter_TypedConfig{
				TypedConfig: protoconv.TypedStructWithFields(PeerMetadataTypeURL, cfg),
			},
		}
	}()
)

func additionalLabels(cfg map[string]any) {
	if additionalLabels := features.MetadataExchangeAdditionalLabels; len(additionalLabels) != 0 {
		cfg["additional_labels"] = additionalLabels
	}
}
