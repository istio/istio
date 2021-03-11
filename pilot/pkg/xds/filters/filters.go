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
	udpa "github.com/cncf/udpa/go/udpa/type/v1"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	cors "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/cors/v3"
	fault "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/fault/v3"
	grpcstats "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/grpc_stats/v3"
	grpcweb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/grpc_web/v3"
	router "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	wasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	httpinspector "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/http_inspector/v3"
	originaldst "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/original_dst/v3"
	originalsrc "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/original_src/v3"
	tlsinspector "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/tls_inspector/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/wasm/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	protobuf "github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/networking/util"
	alpn "istio.io/istio/pkg/envoy/config/filter/http/alpn/v2alpha1"
)

const (
	OriginalSrcFilterName = "envoy.filters.listener.original_src"
	// Alpn HTTP filter name which will override the ALPN for upstream TLS connection.
	AlpnFilterName = "istio.alpn"

	TLSTransportProtocol       = "tls"
	RawBufferTransportProtocol = "raw_buffer"

	MxFilterName = "istio.metadata_exchange"
)

// Define static filters to be reused across the codebase. This avoids duplicate marshaling/unmarshaling
// This should not be used for filters that will be mutated
var (
	Cors = &hcm.HttpFilter{
		Name: wellknown.CORS,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&cors.Cors{}),
		},
	}
	Fault = &hcm.HttpFilter{
		Name: wellknown.Fault,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&fault.HTTPFault{}),
		},
	}
	Router = &hcm.HttpFilter{
		Name: wellknown.Router,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&router.Router{}),
		},
	}
	GrpcWeb = &hcm.HttpFilter{
		Name: wellknown.GRPCWeb,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&grpcweb.GrpcWeb{}),
		},
	}
	GrpcStats = &hcm.HttpFilter{
		Name: wellknown.HTTPGRPCStats,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&grpcstats.FilterConfig{
				EmitFilterState: true,
				PerMethodStatSpecifier: &grpcstats.FilterConfig_StatsForAllMethods{
					StatsForAllMethods: &wrapperspb.BoolValue{Value: false},
				},
			}),
		},
	}
	TLSInspector = &listener.ListenerFilter{
		Name: wellknown.TlsInspector,
		ConfigType: &listener.ListenerFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&tlsinspector.TlsInspector{}),
		},
	}
	HTTPInspector = &listener.ListenerFilter{
		Name: wellknown.HttpInspector,
		ConfigType: &listener.ListenerFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&httpinspector.HttpInspector{}),
		},
	}
	OriginalDestination = &listener.ListenerFilter{
		Name: wellknown.OriginalDestination,
		ConfigType: &listener.ListenerFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&originaldst.OriginalDst{}),
		},
	}
	OriginalSrc = &listener.ListenerFilter{
		Name: OriginalSrcFilterName,
		ConfigType: &listener.ListenerFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&originalsrc.OriginalSrc{
				Mark: 1337,
			}),
		},
	}
	Alpn = &hcm.HttpFilter{
		Name: AlpnFilterName,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&alpn.FilterConfig{
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

	TCPMx = &listener.Filter{
		Name: MxFilterName,
		// TODO: we need to publish this tcp proto: https://github.com/istio/proxy/blob/master/src/envoy/tcp/metadata_exchange/config/metadata_exchange.proto
		// somewhere as go code
		ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(&udpa.TypedStruct{
			TypeUrl: "type.googleapis.com/envoy.tcp.metadataexchange.config.MetadataExchange",
			Value: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"protocol": {
						Kind: &structpb.Value_StringValue{StringValue: "istio-peer-exchange"},
					},
				},
			},
		})},
	}

	HTTPMx = buildHTTPMxFilter()
)

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

func buildHTTPMxFilter() *hcm.HttpFilter {
	var vmConfig *v3.PluginConfig_VmConfig

	if features.EnableWasmTelemetry {
		vmConfig = &v3.PluginConfig_VmConfig{
			VmConfig: &v3.VmConfig{
				Runtime:          "envoy.wasm.runtime.v8",
				AllowPrecompiled: true,
				Code: &core.AsyncDataSource{Specifier: &core.AsyncDataSource_Local{
					Local: &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: "/etc/istio/extensions/metadata-exchange-filter.compiled.wasm",
						},
					},
				}},
			},
		}
	} else {
		vmConfig = &v3.PluginConfig_VmConfig{
			VmConfig: &v3.VmConfig{
				Runtime: "envoy.wasm.runtime.null",
				Code: &core.AsyncDataSource{Specifier: &core.AsyncDataSource_Local{
					Local: &core.DataSource{
						Specifier: &core.DataSource_InlineString{
							InlineString: "envoy.wasm.metadata_exchange",
						},
					},
				}},
			},
		}
	}

	httpMxConfigProto := &wasm.Wasm{
		Config: &v3.PluginConfig{
			Vm:            vmConfig,
			Configuration: util.MessageToAny(&protobuf.StringValue{Value: "{}\n"}),
		},
	}

	typed, err := ptypes.MarshalAny(httpMxConfigProto)
	if err != nil {
		return nil
	}

	return &hcm.HttpFilter{
		Name:       MxFilterName,
		ConfigType: &hcm.HttpFilter_TypedConfig{TypedConfig: typed},
	}
}
