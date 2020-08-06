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
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
	"testing"

	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"google.golang.org/protobuf/testing/protocmp"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	fault "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/fault/v3"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	wellknown "github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/google/go-cmp/cmp"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/config/memory"
	memregistry "istio.io/istio/pilot/pkg/serviceregistry/memory"
	istio_proto "istio.io/istio/pkg/proto"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test/env"
)

var (
	testMesh = meshconfig.MeshConfig{
		ConnectTimeout: &types.Duration{
			Seconds: 10,
			Nanos:   1,
		},
	}
)

func buildEnvoyFilterConfigStore(configPatches []*networking.EnvoyFilter_EnvoyConfigObjectPatch) model.IstioConfigStore {
	store := model.MakeIstioStore(memory.Make(collections.Pilot))

	for i, cp := range configPatches {
		store.Create(model.Config{
			ConfigMeta: model.ConfigMeta{
				Name:             fmt.Sprintf("test-envoyfilter-%d", i),
				Namespace:        "not-default",
				GroupVersionKind: gvk.EnvoyFilter,
			},
			Spec: &networking.EnvoyFilter{
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{cp},
			},
		})
	}
	return store
}

func buildPatchStruct(config string) *types.Struct {
	val := &types.Struct{}
	_ = jsonpb.Unmarshal(strings.NewReader(config), val)
	return val
}

func newTestEnvironment(serviceDiscovery model.ServiceDiscovery, meshConfig meshconfig.MeshConfig,
	configStore model.IstioConfigStore) *model.Environment {
	e := &model.Environment{
		ServiceDiscovery: serviceDiscovery,
		IstioConfigStore: configStore,
		Watcher:          mesh.NewFixedWatcher(&meshConfig),
	}

	e.PushContext = model.NewPushContext()
	_ = e.PushContext.InitContext(e, nil, nil)

	return e
}

func TestApplyListenerPatches(t *testing.T) {
	configPatches := []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
		{
			ApplyTo: networking.EnvoyFilter_LISTENER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				Proxy: &networking.EnvoyFilter_ProxyMatch{
					Metadata: map[string]string{"foo": "sidecar"},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name":"new-outbound-listener1"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_NETWORK_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 12345,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{Name: "filter1"},
						},
					},
				},
				Proxy: &networking.EnvoyFilter_ProxyMatch{
					ProxyVersion: `^1\.[2-9](.*?)$`,
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_INSERT_BEFORE,
				Value:     buildPatchStruct(`{"name":"filter0"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_NETWORK_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 12345,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{Name: "filter1"},
						},
					},
				},
				Proxy: &networking.EnvoyFilter_ProxyMatch{
					ProxyVersion: `^1\.[5-9](.*?)$`,
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_INSERT_FIRST,
				Value:     buildPatchStruct(`{"name":"filter0"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_NETWORK_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 12345,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{Name: "filter2"},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REMOVE,
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_LISTENER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_INBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 12345,
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REMOVE,
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_LISTENER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_INBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 80,
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value:     buildPatchStruct(`{listener_filters: nil}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_FILTER_CHAIN,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_INBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber:  80,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{TransportProtocol: "tls"},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REMOVE,
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_LISTENER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_GATEWAY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 80,
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value:     buildPatchStruct(`{"listener_filters": [{"name":"foo"}]}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_FILTER_CHAIN,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_GATEWAY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 80,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Sni: "*.foo.com",
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value:     buildPatchStruct(`{"filter_chain_match": { "server_names": ["foo.com"] }}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_GATEWAY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 80,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Sni: "*.foo.com",
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name:      wellknown.HTTPConnectionManager,
								SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{Name: "http-filter2"},
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_INSERT_AFTER,
				Value:     buildPatchStruct(`{"name": "http-filter3"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_INBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 80,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name:      wellknown.HTTPConnectionManager,
								SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{Name: "http-filter2"},
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_INSERT_BEFORE,
				Value:     buildPatchStruct(`{"name": "http-filter3"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_INBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 80,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name:      wellknown.HTTPConnectionManager,
								SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{Name: "http-filter2"},
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_INSERT_AFTER,
				Value:     buildPatchStruct(`{"name": "http-filter4"}`),
			},
		},
		// Merge v3 any with v2 any
		{
			ApplyTo: networking.EnvoyFilter_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_INBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 80,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name:      wellknown.HTTPConnectionManager,
								SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{Name: "envoy.fault"}, // Use deprecated name for test.
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value: buildPatchStruct(`
{"name": "envoy.filters.http.fault",
"typed_config": {
        "@type": "type.googleapis.com/envoy.extensions.filters.http.fault.v3.HTTPFault",
        "downstreamNodes": ["foo"]
}
}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_INBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 80,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name:      wellknown.HTTPConnectionManager,
								SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{Name: "http-filter2"},
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_INSERT_FIRST,
				Value:     buildPatchStruct(`{"name": "http-filter0"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_INBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 80,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name: wellknown.HTTPConnectionManager,
								SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{
									Name: wellknown.Fault,
								},
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value: buildPatchStruct(`{"typed_config": {
"@type": "type.googleapis.com/envoy.extensions.filters.http.fault.v3.HTTPFault",
"upstream_cluster": "scooby"}}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_NETWORK_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_INBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 80,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name: wellknown.HTTPConnectionManager,
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value: buildPatchStruct(`
{"name": "envoy.filters.network.http_connection_manager",
 "typed_config": {
        "@type": "type.googleapis.com/envoy.config.filter.network.http_connection_manager.v2.HttpConnectionManager",
         "xffNumTrustedHops": "4"
 }
}`),
			},
		},
		// Ensure we can mix v3 patches with v2 internal
		// Note that alwaysSetRequestIdInResponse is only present in v3 protos. It will be silently ignored
		// as we are working in v2 protos internally
		{
			ApplyTo: networking.EnvoyFilter_NETWORK_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_INBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 80,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name: "envoy.http_connection_manager", // Use deprecated name for test.
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value: buildPatchStruct(`
{"name": "envoy.filters.network.http_connection_manager", 
 "typed_config": {
        "@type": "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager",
         "mergeSlashes": true,
         "alwaysSetRequestIdInResponse": true
 }
}`),
			},
		},
	}

	sidecarOutboundIn := []*listener.Listener{
		{
			Name: "12345",
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 12345,
						},
					},
				},
			},
			FilterChains: []*listener.FilterChain{
				{
					Filters: []*listener.Filter{
						{Name: "filter1"},
						{Name: "filter2"},
					},
				},
			},
		},
		{
			Name: "another-listener",
		},
	}

	sidecarOutboundOut := []*listener.Listener{
		{
			Name: "12345",
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 12345,
						},
					},
				},
			},
			FilterChains: []*listener.FilterChain{
				{
					Filters: []*listener.Filter{
						{Name: "filter0"},
						{Name: "filter1"},
					},
				},
			},
		},
		{
			Name: "another-listener",
		},
		{
			Name: "new-outbound-listener1",
		},
	}

	sidecarOutboundInNoAdd := []*listener.Listener{
		{
			Name: "12345",
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 12345,
						},
					},
				},
			},
			FilterChains: []*listener.FilterChain{
				{
					Filters: []*listener.Filter{
						{Name: "filter1"},
						{Name: "filter2"},
					},
				},
			},
		},
		{
			Name: "another-listener",
		},
	}

	sidecarOutboundOutNoAdd := []*listener.Listener{
		{
			Name: "12345",
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 12345,
						},
					},
				},
			},
			FilterChains: []*listener.FilterChain{
				{
					Filters: []*listener.Filter{
						{Name: "filter0"},
						{Name: "filter1"},
					},
				},
			},
		},
		{
			Name: "another-listener",
		},
	}

	faultFilterIn := &fault.HTTPFault{
		UpstreamCluster: "foobar",
	}
	faultFilterInAny, _ := ptypes.MarshalAny(faultFilterIn)
	faultFilterOut := &fault.HTTPFault{
		UpstreamCluster: "scooby",
		DownstreamNodes: []string{"foo"},
	}
	faultFilterOutAny, _ := ptypes.MarshalAny(faultFilterOut)
	sidecarInboundIn := []*listener.Listener{
		{
			Name: "12345",
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 12345,
						},
					},
				},
			},
		},
		{
			Name: "another-listener",
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 80,
						},
					},
				},
			},
			ListenerFilters: []*listener.ListenerFilter{{Name: "envoy.tls_inspector"}},
			FilterChains: []*listener.FilterChain{
				{
					FilterChainMatch: &listener.FilterChainMatch{TransportProtocol: "tls"},
					TransportSocket: &core.TransportSocket{
						Name: "envoy.transport_sockets.tls",
						ConfigType: &core.TransportSocket_TypedConfig{
							TypedConfig: util.MessageToAny(&tls.DownstreamTlsContext{}),
						},
					},
					Filters: []*listener.Filter{{Name: "network-filter"}},
				},
				{
					Filters: []*listener.Filter{
						{
							Name: wellknown.HTTPConnectionManager,
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: util.MessageToAny(&http_conn.HttpConnectionManager{
									HttpFilters: []*http_conn.HttpFilter{
										{Name: wellknown.Fault,
											ConfigType: &http_conn.HttpFilter_TypedConfig{TypedConfig: faultFilterInAny},
										},
										{Name: "http-filter2"},
									},
								}),
							},
						},
					},
				},
			},
		},
	}

	sidecarInboundOut := []*listener.Listener{
		{
			Name: "another-listener",
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 80,
						},
					},
				},
			},
			ListenerFilters: []*listener.ListenerFilter{{Name: "envoy.tls_inspector"}},
			FilterChains: []*listener.FilterChain{
				{
					Filters: []*listener.Filter{
						{
							Name: wellknown.HTTPConnectionManager,
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: util.MessageToAny(&http_conn.HttpConnectionManager{
									XffNumTrustedHops:            4,
									MergeSlashes:                 true,
									AlwaysSetRequestIdInResponse: true,
									HttpFilters: []*http_conn.HttpFilter{
										{Name: wellknown.Fault,
											ConfigType: &http_conn.HttpFilter_TypedConfig{TypedConfig: faultFilterOutAny},
										},
										{Name: "http-filter3"},
										{Name: "http-filter2"},
										{Name: "http-filter4"},
									},
								}),
							},
						},
					},
				},
			},
		},
	}

	gatewayIn := []*listener.Listener{
		{
			Name: "80",
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 80,
						},
					},
				},
			},
			FilterChains: []*listener.FilterChain{
				{
					FilterChainMatch: &listener.FilterChainMatch{
						ServerNames: []string{"match.com", "*.foo.com"},
					},
					Filters: []*listener.Filter{
						{
							Name: wellknown.HTTPConnectionManager,
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: util.MessageToAny(&http_conn.HttpConnectionManager{
									HttpFilters: []*http_conn.HttpFilter{
										{Name: "http-filter1"},
										{Name: "http-filter2"},
									},
								}),
							},
						},
					},
				},
			},
		},
		{
			Name: "another-listener",
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 443,
						},
					},
				},
			},
			FilterChains: []*listener.FilterChain{
				{
					FilterChainMatch: &listener.FilterChainMatch{
						ServerNames: []string{"nomatch.com", "*.foo.com"},
					},
					Filters: []*listener.Filter{{Name: "network-filter"}},
				},
			},
		},
	}

	gatewayOut := []*listener.Listener{
		{
			Name: "80",
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 80,
						},
					},
				},
			},
			ListenerFilters: []*listener.ListenerFilter{{Name: "foo"}},
			FilterChains: []*listener.FilterChain{
				{
					FilterChainMatch: &listener.FilterChainMatch{
						ServerNames: []string{"match.com", "*.foo.com", "foo.com"},
					},
					Filters: []*listener.Filter{
						{
							Name: wellknown.HTTPConnectionManager,
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: util.MessageToAny(&http_conn.HttpConnectionManager{
									HttpFilters: []*http_conn.HttpFilter{
										{Name: "http-filter1"},
										{Name: "http-filter2"},
										{Name: "http-filter3"},
									},
								}),
							},
						},
					},
				},
			},
		},
		{
			Name: "another-listener",
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 443,
						},
					},
				},
			},
			FilterChains: []*listener.FilterChain{
				{
					FilterChainMatch: &listener.FilterChainMatch{
						ServerNames: []string{"nomatch.com", "*.foo.com"},
					},
					Filters: []*listener.Filter{{Name: "network-filter"}},
				},
			},
		},
	}

	sidecarVirtualInboundIn := []*listener.Listener{
		{
			Name:                                VirtualInboundListenerName,
			HiddenEnvoyDeprecatedUseOriginalDst: istio_proto.BoolTrue,
			TrafficDirection:                    core.TrafficDirection_INBOUND,
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 15006,
						},
					},
				},
			},
			FilterChains: []*listener.FilterChain{
				{
					FilterChainMatch: &listener.FilterChainMatch{
						DestinationPort: &wrappers.UInt32Value{
							Value: 80,
						},
					},
					Filters: []*listener.Filter{
						{
							Name: wellknown.HTTPConnectionManager,
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: util.MessageToAny(&http_conn.HttpConnectionManager{
									HttpFilters: []*http_conn.HttpFilter{
										{Name: wellknown.Fault,
											ConfigType: &http_conn.HttpFilter_TypedConfig{TypedConfig: faultFilterInAny},
										},
										{Name: "http-filter2"},
									},
								}),
							},
						},
					},
				},
			},
		},
	}

	sidecarVirtualInboundOut := []*listener.Listener{
		{
			Name:                                VirtualInboundListenerName,
			HiddenEnvoyDeprecatedUseOriginalDst: istio_proto.BoolTrue,
			TrafficDirection:                    core.TrafficDirection_INBOUND,
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 15006,
						},
					},
				},
			},
			FilterChains: []*listener.FilterChain{
				{
					FilterChainMatch: &listener.FilterChainMatch{
						DestinationPort: &wrappers.UInt32Value{
							Value: 80,
						},
					},
					Filters: []*listener.Filter{
						{
							Name: wellknown.HTTPConnectionManager,
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: util.MessageToAny(&http_conn.HttpConnectionManager{
									XffNumTrustedHops:            4,
									MergeSlashes:                 true,
									AlwaysSetRequestIdInResponse: true,
									HttpFilters: []*http_conn.HttpFilter{
										{Name: wellknown.Fault,
											ConfigType: &http_conn.HttpFilter_TypedConfig{TypedConfig: faultFilterOutAny},
										},
										{Name: "http-filter3"},
										{Name: "http-filter2"},
										{Name: "http-filter4"},
									},
								}),
							},
						},
					},
				},
			},
		},
	}

	sidecarProxy := &model.Proxy{
		Type:            model.SidecarProxy,
		ConfigNamespace: "not-default",
		Metadata: &model.NodeMetadata{
			IstioVersion: "1.2.2",
			Raw: map[string]interface{}{
				"foo": "sidecar",
				"bar": "proxy",
			},
		},
	}

	gatewayProxy := &model.Proxy{
		Type:            model.Router,
		ConfigNamespace: "not-default",
		Metadata: &model.NodeMetadata{
			IstioVersion: "1.2.2",
			Raw: map[string]interface{}{
				"foo": "sidecar",
				"bar": "proxy",
			},
		},
	}
	serviceDiscovery := memregistry.NewServiceDiscovery(nil)
	e := newTestEnvironment(serviceDiscovery, testMesh, buildEnvoyFilterConfigStore(configPatches))
	push := model.NewPushContext()
	_ = push.InitContext(e, nil, nil)

	type args struct {
		patchContext networking.EnvoyFilter_PatchContext
		proxy        *model.Proxy
		push         *model.PushContext
		listeners    []*listener.Listener
		skipAdds     bool
	}
	tests := []struct {
		name string
		args args
		want []*listener.Listener
	}{
		{
			name: "sidecar inbound lds",
			args: args{
				patchContext: networking.EnvoyFilter_SIDECAR_INBOUND,
				proxy:        sidecarProxy,
				push:         push,
				listeners:    sidecarInboundIn,
				skipAdds:     false,
			},
			want: sidecarInboundOut,
		},
		{
			name: "gateway lds",
			args: args{
				patchContext: networking.EnvoyFilter_GATEWAY,
				proxy:        gatewayProxy,
				push:         push,
				listeners:    gatewayIn,
				skipAdds:     false,
			},
			want: gatewayOut,
		},
		{
			name: "sidecar outbound lds",
			args: args{
				patchContext: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				proxy:        sidecarProxy,
				push:         push,
				listeners:    sidecarOutboundIn,
				skipAdds:     false,
			},
			want: sidecarOutboundOut,
		},
		{
			name: "sidecar outbound lds - skip adds",
			args: args{
				patchContext: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				proxy:        sidecarProxy,
				push:         push,
				listeners:    sidecarOutboundInNoAdd,
				skipAdds:     true,
			},
			want: sidecarOutboundOutNoAdd,
		},
		{
			name: "sidecar inbound virtual",
			args: args{
				patchContext: networking.EnvoyFilter_SIDECAR_INBOUND,
				proxy:        sidecarProxy,
				push:         push,
				listeners:    sidecarVirtualInboundIn,
				skipAdds:     false,
			},
			want: sidecarVirtualInboundOut,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplyListenerPatches(tt.args.patchContext, tt.args.proxy, tt.args.push,
				tt.args.listeners, tt.args.skipAdds)
			if diff := cmp.Diff(tt.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("ApplyListenerPatches(): %s mismatch (-want +got):\n%s", tt.name, diff)
			}
		})
	}
}

// This benchmark measures the performance of Telemetry V2 EnvoyFilter patches. The intent here is to
// measure overhead of using EnvoyFilters rather than native code.
func BenchmarkTelemetryV2Filters(b *testing.B) {
	l := &listener.Listener{
		Name: "another-listener",
		Address: &core.Address{
			Address: &core.Address_SocketAddress{
				SocketAddress: &core.SocketAddress{
					PortSpecifier: &core.SocketAddress_PortValue{
						PortValue: 80,
					},
				},
			},
		},
		ListenerFilters: []*listener.ListenerFilter{{Name: "envoy.tls_inspector"}},
		FilterChains: []*listener.FilterChain{
			{
				Filters: []*listener.Filter{
					{
						Name: wellknown.HTTPConnectionManager,
						ConfigType: &listener.Filter_TypedConfig{
							TypedConfig: util.MessageToAny(&http_conn.HttpConnectionManager{
								XffNumTrustedHops:            4,
								MergeSlashes:                 true,
								AlwaysSetRequestIdInResponse: true,
								HttpFilters: []*http_conn.HttpFilter{
									{Name: "http-filter3"},
									{Name: "envoy.router"}, // Use deprecated name for test.
									{Name: "http-filter2"},
								},
							}),
						},
					},
				},
			},
		},
	}

	file, err := ioutil.ReadFile(filepath.Join(env.IstioSrc, "manifests/charts/istio-control/istio-discovery/files/gen-istio.yaml"))
	if err != nil {
		b.Fatalf("failed to read telemetry v2 Envoy Filters")
	}
	var configPatches []*networking.EnvoyFilter_EnvoyConfigObjectPatch

	configs, _, err := crd.ParseInputs(string(file))
	if err != nil {
		b.Fatalf("failed to unmarshal EnvoyFilter: %v", err)
	}
	for _, c := range configs {
		if c.GroupVersionKind != gvk.EnvoyFilter {
			continue
		}
		configPatches = append(configPatches, c.Spec.(*networking.EnvoyFilter).ConfigPatches...)
	}
	if len(configPatches) == 0 {
		b.Fatalf("found no patches, failed to read telemetry config?")
	}

	sidecarProxy := &model.Proxy{
		Type:            model.SidecarProxy,
		ConfigNamespace: "not-default",
		Metadata: &model.NodeMetadata{
			IstioVersion: "1.2.2",
			Raw: map[string]interface{}{
				"foo": "sidecar",
				"bar": "proxy",
			},
		},
	}
	serviceDiscovery := memregistry.NewServiceDiscovery(nil)
	e := newTestEnvironment(serviceDiscovery, testMesh, buildEnvoyFilterConfigStore(configPatches))
	push := model.NewPushContext()
	_ = push.InitContext(e, nil, nil)

	var got interface{}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		copied := proto.Clone(l)
		got = ApplyListenerPatches(networking.EnvoyFilter_SIDECAR_OUTBOUND, sidecarProxy, push,
			[]*listener.Listener{copied.(*listener.Listener)}, false)
	}
	_ = got
}
