// Copyright 2020 Istio Authors
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

package v1alpha3

import (
	listener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	cors "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/cors/v2"
	fault "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/fault/v2"
	grpcweb "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/grpc_web/v2"
	router "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/router/v2"
	httpinspector "github.com/envoyproxy/go-control-plane/envoy/config/filter/listener/http_inspector/v2"
	originaldst "github.com/envoyproxy/go-control-plane/envoy/config/filter/listener/original_dst/v2"
	originalsrc "github.com/envoyproxy/go-control-plane/envoy/config/filter/listener/original_src/v2alpha1"
	tlsinspector "github.com/envoyproxy/go-control-plane/envoy/config/filter/listener/tls_inspector/v2"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/ptypes/wrappers"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/networking/util"
)

const OriginalSrc = "envoy.listener.original_src"

// Define static filters to be reused across the codebase. This avoids duplicate marshaling/unmarshaling
// This should not be used for filters that will be mutated
var (
	corsFilter = &http_conn.HttpFilter{
		Name: wellknown.CORS,
		ConfigType: &http_conn.HttpFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&cors.Cors{}),
		},
	}
	faultFilter = &http_conn.HttpFilter{
		Name: wellknown.Fault,
		ConfigType: &http_conn.HttpFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&fault.HTTPFault{}),
		},
	}
	routerFilter = &http_conn.HttpFilter{
		Name: wellknown.Router,
		ConfigType: &http_conn.HttpFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&router.Router{
				DynamicStats: &wrappers.BoolValue{Value: features.EnableProxyDynamicStats},
			}),
		},
	}
	grpcWebFilter = &http_conn.HttpFilter{
		Name: wellknown.GRPCWeb,
		ConfigType: &http_conn.HttpFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&grpcweb.GrpcWeb{}),
		},
	}

	tlsInspectorFilter = &listener.ListenerFilter{
		Name: wellknown.TlsInspector,
		ConfigType: &listener.ListenerFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&tlsinspector.TlsInspector{}),
		},
	}
	httpInspectorFilter = &listener.ListenerFilter{
		Name: wellknown.HttpInspector,
		ConfigType: &listener.ListenerFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&httpinspector.HttpInspector{}),
		},
	}
	originalDestinationFilter = &listener.ListenerFilter{
		Name: wellknown.OriginalDestination,
		ConfigType: &listener.ListenerFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&originaldst.OriginalDst{}),
		},
	}
	originalSrcFilter = &listener.ListenerFilter{
		Name: OriginalSrc,
		ConfigType: &listener.ListenerFilter_TypedConfig{
			TypedConfig: util.MessageToAny(&originalsrc.OriginalSrc{}),
		},
	}
)
