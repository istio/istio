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
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	cors "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/cors/v3"
	fault "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/fault/v3"
	grpcweb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/grpc_web/v3"
	router "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	httpinspector "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/http_inspector/v3"
	originaldst "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/original_dst/v3"
	originalsrc "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/original_src/v3"
	tlsinspector "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/tls_inspector/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"

	"istio.io/istio/pilot/pkg/networking/util"
	alpn "istio.io/istio/pkg/envoy/config/filter/http/alpn/v2alpha1"
)

const (
	OriginalSrcFilterName = "envoy.listener.original_src"
	// Alpn HTTP filter name which will override the ALPN for upstream TLS connection.
	AlpnFilterName = "istio.alpn"

	// DNSListenerFilterName is the name of UDP listener filter for resolving DNS queries
	DNSListenerFilterName = "envoy.filters.udp.dns_filter"
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
			TypedConfig: util.MessageToAny(&originalsrc.OriginalSrc{}),
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
)

var (
	// These ALPNs are injected in the client side by the ALPN filter.
	// "istio" is added for each upstream protocol in order to make it
	// backward compatible. e.g., 1.4 proxy -> 1.3 proxy.
	mtlsHTTP10ALPN = []string{"istio-http/1.0", "istio"}
	mtlsHTTP11ALPN = []string{"istio-http/1.1", "istio"}
	mtlsHTTP2ALPN  = []string{"istio-h2", "istio"}
)
