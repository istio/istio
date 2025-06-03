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
	"testing"
	"time"

	udpa "github.com/cncf/xds/go/udpa/type/v1"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	fault "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/fault/v3"
	tlsinspector "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/tls_inspector/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	redis "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/redis_proxy/v3"
	tcp_proxy "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	memregistry "istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pilot/pkg/util/protoconv"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/log"
	istio_proto "istio.io/istio/pkg/proto"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/wellknown"
)

var testMesh = &meshconfig.MeshConfig{
	ConnectTimeout: &durationpb.Duration{
		Seconds: 10,
		Nanos:   1,
	},
}

func buildEnvoyFilterConfigStore(configPatches []*networking.EnvoyFilter_EnvoyConfigObjectPatch) model.ConfigStore {
	store := memory.Make(collections.Pilot)

	for i, cp := range configPatches {
		_, err := store.Create(config.Config{
			Meta: config.Meta{
				Name:             fmt.Sprintf("test-envoyfilter-%d", i),
				Namespace:        "not-default",
				GroupVersionKind: gvk.EnvoyFilter,
			},
			Spec: &networking.EnvoyFilter{
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{cp},
			},
		})
		if err != nil {
			log.Errorf("create envoyfilter failed %v", err)
		}
	}
	return store
}

func buildEnvoyFilterConfigStoreWithPriorities(configPatches []*networking.EnvoyFilter_EnvoyConfigObjectPatch, priorities []int32) model.ConfigStore {
	store := memory.Make(collections.Pilot)

	for i, cp := range configPatches {
		_, err := store.Create(config.Config{
			Meta: config.Meta{
				Name:             fmt.Sprintf("test-envoyfilter-%d", i),
				Namespace:        "not-default",
				GroupVersionKind: gvk.EnvoyFilter,
			},
			Spec: &networking.EnvoyFilter{
				ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{cp},
				Priority:      priorities[i],
			},
		})
		if err != nil {
			log.Errorf("create envoyfilter failed %v", err)
		}
	}
	return store
}

func buildPatchStruct(config string) *structpb.Struct {
	val := &structpb.Struct{}
	_ = protomarshal.UnmarshalString(config, val)
	return val
}

// nolint: unparam
func buildGolangPatchStruct(config string) *structpb.Struct {
	val := &structpb.Struct{}
	_ = protomarshal.Unmarshal([]byte(config), val)
	return val
}

func newTestEnvironment(serviceDiscovery model.ServiceDiscovery, meshConfig *meshconfig.MeshConfig,
	configStore model.ConfigStore,
) *model.Environment {
	e := &model.Environment{
		ServiceDiscovery: serviceDiscovery,
		ConfigStore:      configStore,
		Watcher:          meshwatcher.NewTestWatcher(meshConfig),
	}

	pushContext := model.NewPushContext()
	e.Init()
	pushContext.InitContext(e, nil, nil)
	e.SetPushContext(pushContext)
	return e
}

func TestApplyListenerPatches(t *testing.T) {
	configPatchesPriorities := []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
		{
			ApplyTo: networking.EnvoyFilter_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_GATEWAY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 80,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name:      wellknown.HTTPConnectionManager,
								SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{Name: "http-filter-to-be-removed-then-add"},
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name":"http-filter-to-be-removed-then-add"}`),
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
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name:      wellknown.HTTPConnectionManager,
								SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{Name: "http-filter-to-be-removed-then-add"},
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REMOVE,
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_LISTENER_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_GATEWAY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber:     80,
						ListenerFilter: "envoy.filters.listener.tls_inspector",
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value: buildPatchStruct(`{
    "name": "envoy.filters.listener.tls_inspector",
    "typedConfig": {
        "@type": "type.googleapis.com/envoy.extensions.filters.listener.tls_inspector.v3.TlsInspector",
        "initialReadBufferSize": 8192
    }
}`),
			},
		},
	}
	priorities := []int32{
		2, 1, 0,
	}

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
			ApplyTo: networking.EnvoyFilter_NETWORK_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{Name: wellknown.RedisProxy},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value: buildPatchStruct(`
{"name": "envoy.filters.network.redis_proxy",
"typed_config": {
        "@type": "type.googleapis.com/envoy.extensions.filters.network.redis_proxy.v3.RedisProxy",
        "settings": {"op_timeout": "0.2s"}
}
}`),
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
				Context: networking.EnvoyFilter_GATEWAY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 80,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name:      wellknown.HTTPConnectionManager,
								SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{Name: "http-filter-to-be-removed-then-add"},
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REMOVE,
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
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name:      wellknown.HTTPConnectionManager,
								SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{Name: "http-filter-to-be-removed-then-add"},
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name":"http-filter-to-be-removed-then-add"}`),
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
		// Remove inline http filter patch
		{
			ApplyTo: networking.EnvoyFilter_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_INBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name:      wellknown.HTTPConnectionManager,
								SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{Name: "http-filter3"},
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_INSERT_BEFORE,
				Value:     buildPatchStruct(`{"name": "http-filter-to-be-removed2"}`),
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
								SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{Name: "http-filter-to-be-removed2"},
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REMOVE,
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
								SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{Name: "http-filter-to-be-removed"},
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REMOVE,
			},
		},
		// remove then add
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
								SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{Name: "http-filter-to-be-removed-then-add"},
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REMOVE,
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
								SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{Name: "http-filter-to-be-removed-then-add"},
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name":"http-filter-to-be-removed-then-add"}`),
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
								SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{Name: "envoy.filters.http.fault"},
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
        "@type": "type.googleapis.com/type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager",
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
        "@type": "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager",
         "mergeSlashes": true,
         "alwaysSetRequestIdInResponse": true
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
				Operation: networking.EnvoyFilter_Patch_INSERT_AFTER,
				Value:     buildPatchStruct(`{"name": "http-filter-5"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_NETWORK_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 6379,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name: "filter1",
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REPLACE,
				Value: buildPatchStruct(`
{"name": "envoy.redis_proxy",
 "typed_config": {
        "@type": "type.googleapis.com/envoy.extensions.filters.network.redis_proxy.v3.RedisProxy",
         "stat_prefix": "redis_stats",
         "prefix_routes": {
             "catch_all_route": {
                 "cluster": "custom-redis-cluster"
             }
         }
 }
}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_NETWORK_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 6381,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name: "default-network-filter",
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REPLACE,
				Value:     buildPatchStruct(`{"name": "default-network-filter-replaced"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_NETWORK_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 6381,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name: "default-network-filter-removed",
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REMOVE,
			},
		},
		// This patch should not be applied because network filter name doesn't match
		{
			ApplyTo: networking.EnvoyFilter_NETWORK_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 6380,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name: "network-filter-should-not-be-replaced-not-match",
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REPLACE,
				Value:     buildPatchStruct(`{"name": "network-filter-replaced-should-not-be-applied"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						Name: "listener-http-filter-to-be-replaced",
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name: wellknown.HTTPConnectionManager,
								SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{
									Name: "http-filter-to-be-replaced",
								},
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REPLACE,
				Value:     buildPatchStruct(`{"name": "http-filter-replaced"}`),
			},
		},
		// This patch should not be applied because the subfilter name doesn't match
		{
			ApplyTo: networking.EnvoyFilter_HTTP_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						Name: "listener-http-filter-to-be-replaced-not-found",
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name: wellknown.HTTPConnectionManager,
								SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{
									Name: "http-filter-should-not-be-replaced-not-match",
								},
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REPLACE,
				Value:     buildPatchStruct(`{"name": "http-filter-replaced-should-not-be-applied"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_NETWORK_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						Name: model.VirtualInboundListenerName,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							DestinationPort: 6380,
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name: "network-filter-to-be-replaced",
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REPLACE,
				Value: buildPatchStruct(`
{"name": "envoy.redis_proxy",
 "typed_config": {
        "@type": "type.googleapis.com/envoy.extensions.filters.network.redis_proxy.v3.RedisProxy",
         "stat_prefix": "redis_stats",
         "prefix_routes": {
             "catch_all_route": {
                 "cluster": "custom-redis-cluster"
             }
         }
 }
}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_FILTER_CHAIN,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber:  12345,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{TransportProtocol: "tls"},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value: buildPatchStruct(`
					{"transport_socket":{
						"name":"envoy.transport_sockets.tls",
						"typed_config":{
							"@type":"type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.DownstreamTlsContext",
							"common_tls_context":{
								"tls_params":{
									"tls_maximum_protocol_version":"TLSv1_3",
									"tls_minimum_protocol_version":"TLSv1_2"}}}}}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_FILTER_CHAIN,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber:  12345,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{ApplicationProtocols: "http/1.1"},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REMOVE,
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_LISTENER_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
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
			ApplyTo: networking.EnvoyFilter_LISTENER_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 12345,
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name":"http-filter-to-be-removed-then-add"}`),
			},
		},
		// Patch custom TLS type
		{
			ApplyTo: networking.EnvoyFilter_FILTER_CHAIN,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
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
		// Patch custom TLS type to a FC without TLS already set
		{
			ApplyTo: networking.EnvoyFilter_FILTER_CHAIN,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 7778,
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
		{
			ApplyTo: networking.EnvoyFilter_NETWORK_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						Name: model.VirtualInboundListenerName,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Name: "filter-chain-name-match",
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name: "custom-network-filter-2",
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REMOVE,
			},
		},
		// Remove inline network filter patch
		{
			ApplyTo: networking.EnvoyFilter_NETWORK_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						Name: model.VirtualInboundListenerName,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Name: "filter-chain-name-match",
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name: "custom-network-filter-1",
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_INSERT_BEFORE,
				Value:     buildPatchStruct(`{"name":"network-filter-to-be-removed"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_NETWORK_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						Name: model.VirtualInboundListenerName,
						FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Name: "filter-chain-name-match",
							Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
								Name: "network-filter-to-be-removed",
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REMOVE,
			},
		},
		// Add transport socket to virtual inbound.
		{
			ApplyTo: networking.EnvoyFilter_FILTER_CHAIN,
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
				Value: buildPatchStruct(`
					{"transport_socket":{
						"name":"envoy.transport_sockets.tls",
						"typed_config":{
							"@type":"type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.DownstreamTlsContext",
							"common_tls_context":{
								"alpn_protocols": [ "h2-80", "http/1.1-80" ]}}}}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_FILTER_CHAIN,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_INBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 6380,
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value: buildPatchStruct(`
					{"transport_socket":{
						"name":"envoy.transport_sockets.tls",
						"typed_config":{
							"@type":"type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.DownstreamTlsContext",
							"common_tls_context":{
								"alpn_protocols": [ "h2-6380", "http/1.1-6380" ]}}}}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_LISTENER_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber: 12345,
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name":"envoy.filters.listener.proxy_protocol"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_LISTENER_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber:     12345,
						ListenerFilter: "envoy.filters.listener.proxy_protocol",
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_INSERT_BEFORE,
				Value:     buildPatchStruct(`{"name":"before proxy_protocol"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_LISTENER_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber:     12345,
						ListenerFilter: "envoy.filters.listener.proxy_protocol",
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_INSERT_AFTER,
				Value:     buildPatchStruct(`{"name":"after proxy_protocol"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_LISTENER_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber:     6381,
						ListenerFilter: "filter-to-be-removed",
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REMOVE,
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_LISTENER_FILTER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &networking.EnvoyFilter_ListenerMatch{
						PortNumber:     6381,
						ListenerFilter: "filter-before-replace",
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REPLACE,
				Value:     buildPatchStruct(`{"name":"filter-after-replace"}`),
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
					FilterChainMatch: &listener.FilterChainMatch{TransportProtocol: "tls"},
					TransportSocket: &core.TransportSocket{
						Name: "envoy.transport_sockets.tls",
						ConfigType: &core.TransportSocket_TypedConfig{
							TypedConfig: protoconv.MessageToAny(&tls.DownstreamTlsContext{
								CommonTlsContext: &tls.CommonTlsContext{
									TlsParams: &tls.TlsParameters{
										EcdhCurves:                []string{"X25519"},
										TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_1,
									},
								},
							}),
						},
					},
					Filters: []*listener.Filter{{Name: "envoy.transport_sockets.tls"}},
				},
				{
					FilterChainMatch: &listener.FilterChainMatch{ApplicationProtocols: []string{"http/1.1", "h2c"}},
					Filters: []*listener.Filter{
						{Name: "filter1"},
					},
				},
				{
					Filters: []*listener.Filter{
						{Name: "filter1"},
						{Name: "filter2"},
					},
				},
			},
		},
		{
			Name: "network-filter-to-be-replaced",
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 6379,
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
			Name: "redis-proxy",
			FilterChains: []*listener.FilterChain{
				{
					FilterChainMatch: &listener.FilterChainMatch{
						DestinationPort: &wrapperspb.UInt32Value{
							Value: 9999,
						},
					},
					Filters: []*listener.Filter{
						{
							Name: wellknown.RedisProxy,
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: protoconv.MessageToAny(&redis.RedisProxy{
									Settings: &redis.RedisProxy_ConnPoolSettings{
										OpTimeout: durationpb.New(time.Second * 5),
									},
								}),
							},
						},
					},
				},
			},
		},
		{
			Name: "network-filter-to-be-replaced-not-found",
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 6380,
						},
					},
				},
			},
			FilterChains: []*listener.FilterChain{
				{
					Filters: []*listener.Filter{
						{Name: "network-filter-should-not-be-replaced"},
					},
				},
			},
		},
		{
			Name: "default-filter-chain",
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 6381,
						},
					},
				},
			},
			FilterChains: []*listener.FilterChain{
				{
					Filters: []*listener.Filter{
						{Name: "network-filter"},
					},
				},
			},
			DefaultFilterChain: &listener.FilterChain{
				Filters: []*listener.Filter{
					{Name: "default-network-filter"},
					{Name: "default-network-filter-removed"},
				},
			},
			ListenerFilters: []*listener.ListenerFilter{
				{
					Name: "filter-to-be-removed",
				},
				{
					Name: "filter-before-replace",
				},
			},
		},
		{
			Name: "another-listener",
		},
		{
			Name: "listener-http-filter-to-be-replaced",
			FilterChains: []*listener.FilterChain{
				{
					FilterChainMatch: &listener.FilterChainMatch{
						DestinationPort: &wrapperspb.UInt32Value{
							Value: 80,
						},
					},
					Filters: []*listener.Filter{
						{
							Name: wellknown.HTTPConnectionManager,
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: protoconv.MessageToAny(&hcm.HttpConnectionManager{
									HttpFilters: []*hcm.HttpFilter{
										{Name: "http-filter-to-be-replaced"},
										{Name: "another-http-filter"},
									},
								}),
							},
						},
					},
				},
			},
		},
		{
			Name: "listener-http-filter-to-be-replaced-not-found",
			FilterChains: []*listener.FilterChain{
				{
					FilterChainMatch: &listener.FilterChainMatch{
						DestinationPort: &wrapperspb.UInt32Value{
							Value: 80,
						},
					},
					Filters: []*listener.Filter{
						{
							Name: wellknown.HTTPConnectionManager,
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: protoconv.MessageToAny(&hcm.HttpConnectionManager{
									HttpFilters: []*hcm.HttpFilter{
										{Name: "http-filter-should-not-be-replaced"},
										{Name: "another-http-filter"},
									},
								}),
							},
						},
					},
				},
			},
		},
		{
			Name: "custom-tls-replacement",
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 7777,
						},
					},
				},
			},
			FilterChains: []*listener.FilterChain{
				{
					TransportSocket: &core.TransportSocket{
						Name: "envoy.transport_sockets.tls",
						ConfigType: &core.TransportSocket_TypedConfig{
							TypedConfig: protoconv.MessageToAny(&tls.DownstreamTlsContext{
								CommonTlsContext: &tls.CommonTlsContext{
									TlsParams: &tls.TlsParameters{},
								},
							}),
						},
					},
					Filters: []*listener.Filter{{Name: "filter"}},
				},
			},
		},
		{
			Name: "custom-tls-addition",
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 7778,
						},
					},
				},
			},
			FilterChains: []*listener.FilterChain{
				{
					Filters: []*listener.Filter{{Name: "filter"}},
				},
			},
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
					FilterChainMatch: &listener.FilterChainMatch{TransportProtocol: "tls"},
					TransportSocket: &core.TransportSocket{
						Name: "envoy.transport_sockets.tls",
						ConfigType: &core.TransportSocket_TypedConfig{
							TypedConfig: protoconv.MessageToAny(&tls.DownstreamTlsContext{
								CommonTlsContext: &tls.CommonTlsContext{
									TlsParams: &tls.TlsParameters{
										EcdhCurves:                []string{"X25519"},
										TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
										TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
									},
								},
							}),
						},
					},
					Filters: []*listener.Filter{{Name: "envoy.transport_sockets.tls"}},
				},
				{
					Filters: []*listener.Filter{
						{Name: "filter0"},
						{Name: "filter1"},
					},
				},
			},
			ListenerFilters: []*listener.ListenerFilter{
				{
					Name: "http-filter-to-be-removed-then-add",
				},
				{
					Name: "before proxy_protocol",
				},
				{
					Name: "envoy.filters.listener.proxy_protocol",
				},
				{
					Name: "after proxy_protocol",
				},
			},
		},
		{
			Name: "network-filter-to-be-replaced",
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 6379,
						},
					},
				},
			},
			FilterChains: []*listener.FilterChain{
				{
					Filters: []*listener.Filter{
						{
							Name: "envoy.redis_proxy",
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: protoconv.MessageToAny(&redis.RedisProxy{
									StatPrefix: "redis_stats",
									PrefixRoutes: &redis.RedisProxy_PrefixRoutes{
										CatchAllRoute: &redis.RedisProxy_PrefixRoutes_Route{
											Cluster: "custom-redis-cluster",
										},
									},
								}),
							},
						},
						{Name: "filter2"},
					},
				},
			},
		},
		{
			Name: "redis-proxy",
			FilterChains: []*listener.FilterChain{
				{
					FilterChainMatch: &listener.FilterChainMatch{
						DestinationPort: &wrapperspb.UInt32Value{
							Value: 9999,
						},
					},
					Filters: []*listener.Filter{
						{
							Name: wellknown.RedisProxy,
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: protoconv.MessageToAny(&redis.RedisProxy{
									Settings: &redis.RedisProxy_ConnPoolSettings{
										OpTimeout: durationpb.New(time.Millisecond * 200),
									},
								}),
							},
						},
					},
				},
			},
		},
		{
			Name: "network-filter-to-be-replaced-not-found",
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 6380,
						},
					},
				},
			},
			FilterChains: []*listener.FilterChain{
				{
					Filters: []*listener.Filter{
						{Name: "network-filter-should-not-be-replaced"},
					},
				},
			},
		},
		{
			Name: "default-filter-chain",
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 6381,
						},
					},
				},
			},
			FilterChains: []*listener.FilterChain{
				{
					Filters: []*listener.Filter{
						{Name: "network-filter"},
					},
				},
			},
			DefaultFilterChain: &listener.FilterChain{
				Filters: []*listener.Filter{
					{Name: "default-network-filter-replaced"},
				},
			},
			ListenerFilters: []*listener.ListenerFilter{
				{
					Name: "filter-after-replace",
				},
			},
		},
		{
			Name: "another-listener",
		},
		{
			Name: "listener-http-filter-to-be-replaced",
			FilterChains: []*listener.FilterChain{
				{
					FilterChainMatch: &listener.FilterChainMatch{
						DestinationPort: &wrapperspb.UInt32Value{
							Value: 80,
						},
					},
					Filters: []*listener.Filter{
						{
							Name: wellknown.HTTPConnectionManager,
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: protoconv.MessageToAny(&hcm.HttpConnectionManager{
									HttpFilters: []*hcm.HttpFilter{
										{Name: "http-filter-replaced"},
										{Name: "another-http-filter"},
									},
								}),
							},
						},
					},
				},
			},
		},
		{
			Name: "listener-http-filter-to-be-replaced-not-found",
			FilterChains: []*listener.FilterChain{
				{
					FilterChainMatch: &listener.FilterChainMatch{
						DestinationPort: &wrapperspb.UInt32Value{
							Value: 80,
						},
					},
					Filters: []*listener.Filter{
						{
							Name: wellknown.HTTPConnectionManager,
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: protoconv.MessageToAny(&hcm.HttpConnectionManager{
									HttpFilters: []*hcm.HttpFilter{
										{Name: "http-filter-should-not-be-replaced"},
										{Name: "another-http-filter"},
									},
								}),
							},
						},
					},
				},
			},
		},
		{
			Name: "custom-tls-replacement",
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 7777,
						},
					},
				},
			},
			FilterChains: []*listener.FilterChain{
				{
					TransportSocket: &core.TransportSocket{
						Name: "transport_sockets.alts",
						ConfigType: &core.TransportSocket_TypedConfig{
							TypedConfig: protoconv.MessageToAny(&udpa.TypedStruct{
								TypeUrl: "type.googleapis.com/envoy.extensions.transport_sockets.alts.v3.Alts",
								Value:   buildGolangPatchStruct(`{"handshaker_service":"1.2.3.4"}`),
							}),
						},
					},
					Filters: []*listener.Filter{{Name: "filter"}},
				},
			},
		},
		{
			Name: "custom-tls-addition",
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 7778,
						},
					},
				},
			},
			FilterChains: []*listener.FilterChain{
				{
					TransportSocket: &core.TransportSocket{
						Name: "transport_sockets.alts",
						ConfigType: &core.TransportSocket_TypedConfig{
							TypedConfig: protoconv.MessageToAny(&udpa.TypedStruct{
								TypeUrl: "type.googleapis.com/envoy.extensions.transport_sockets.alts.v3.Alts",
								Value:   buildGolangPatchStruct(`{"handshaker_service":"1.2.3.4"}`),
							}),
						},
					},
					Filters: []*listener.Filter{{Name: "filter"}},
				},
			},
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
					FilterChainMatch: &listener.FilterChainMatch{TransportProtocol: "tls"},
					TransportSocket: &core.TransportSocket{
						Name: "envoy.transport_sockets.tls",
						ConfigType: &core.TransportSocket_TypedConfig{
							TypedConfig: protoconv.MessageToAny(&tls.DownstreamTlsContext{}),
						},
					},
					Filters: []*listener.Filter{{Name: "envoy.transport_sockets.tls"}},
				},
				{
					FilterChainMatch: &listener.FilterChainMatch{ApplicationProtocols: []string{"http/1.1", "h2c"}},
					Filters: []*listener.Filter{
						{Name: "filter1"},
					},
				},
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
					FilterChainMatch: &listener.FilterChainMatch{TransportProtocol: "tls"},
					TransportSocket: &core.TransportSocket{
						Name: "envoy.transport_sockets.tls",
						ConfigType: &core.TransportSocket_TypedConfig{
							TypedConfig: protoconv.MessageToAny(&tls.DownstreamTlsContext{
								CommonTlsContext: &tls.CommonTlsContext{
									TlsParams: &tls.TlsParameters{
										TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
										TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_2,
									},
								},
							}),
						},
					},
					Filters: []*listener.Filter{{Name: "envoy.transport_sockets.tls"}},
				},
				{
					Filters: []*listener.Filter{
						{Name: "filter0"},
						{Name: "filter1"},
					},
				},
			},
			ListenerFilters: []*listener.ListenerFilter{
				{
					Name: "http-filter-to-be-removed-then-add",
				},
				{
					Name: "before proxy_protocol",
				},
				{
					Name: "envoy.filters.listener.proxy_protocol",
				},
				{
					Name: "after proxy_protocol",
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
	faultFilterInAny := protoconv.MessageToAny(faultFilterIn)
	faultFilterOut := &fault.HTTPFault{
		UpstreamCluster: "scooby",
		DownstreamNodes: []string{"foo"},
	}
	faultFilterOutAny := protoconv.MessageToAny(faultFilterOut)

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
								TypedConfig: protoconv.MessageToAny(&hcm.HttpConnectionManager{
									HttpFilters: []*hcm.HttpFilter{
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
								TypedConfig: protoconv.MessageToAny(&hcm.HttpConnectionManager{
									HttpFilters: []*hcm.HttpFilter{
										{Name: "http-filter1"},
										{Name: "http-filter2"},
										{Name: "http-filter3"},
										{Name: "http-filter-to-be-removed-then-add"},
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

	gatewayPriorityIn := []*listener.Listener{
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
					Filters: []*listener.Filter{
						{
							Name: wellknown.HTTPConnectionManager,
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: protoconv.MessageToAny(&hcm.HttpConnectionManager{
									HttpFilters: []*hcm.HttpFilter{},
								}),
							},
						},
					},
				},
			},
			ListenerFilters: []*listener.ListenerFilter{
				{Name: "envoy.filters.listener.tls_inspector", ConfigType: xdsfilters.TLSInspector.ConfigType},
			},
		},
	}

	gatewayPriorityOut := []*listener.Listener{
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
					Filters: []*listener.Filter{
						{
							Name: wellknown.HTTPConnectionManager,
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: protoconv.MessageToAny(&hcm.HttpConnectionManager{
									HttpFilters: []*hcm.HttpFilter{
										{Name: "http-filter-to-be-removed-then-add"},
									},
								}),
							},
						},
					},
				},
			},
			ListenerFilters: []*listener.ListenerFilter{
				{
					Name: "envoy.filters.listener.tls_inspector",
					ConfigType: &listener.ListenerFilter_TypedConfig{
						TypedConfig: protoconv.MessageToAny(&tlsinspector.TlsInspector{
							InitialReadBufferSize: &wrapperspb.UInt32Value{Value: 8192},
						}),
					},
				},
			},
		},
	}

	sidecarVirtualInboundIn := []*listener.Listener{
		{
			Name:             model.VirtualInboundListenerName,
			UseOriginalDst:   istio_proto.BoolTrue,
			TrafficDirection: core.TrafficDirection_INBOUND,
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
					Name: "virtualInbound-blackhole",
					FilterChainMatch: &listener.FilterChainMatch{
						DestinationPort: &wrapperspb.UInt32Value{
							Value: 15006,
						},
					},
					Filters: []*listener.Filter{
						{
							Name: wellknown.TCPProxy,
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: protoconv.MessageToAny(&tcp_proxy.TcpProxy{
									StatPrefix:       util.BlackHoleCluster,
									ClusterSpecifier: &tcp_proxy.TcpProxy_Cluster{Cluster: util.BlackHoleCluster},
								}),
							},
						},
					},
				},
				{
					FilterChainMatch: &listener.FilterChainMatch{
						DestinationPort: &wrapperspb.UInt32Value{
							Value: 80,
						},
					},
					Filters: []*listener.Filter{
						{
							Name: wellknown.HTTPConnectionManager,
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: protoconv.MessageToAny(&hcm.HttpConnectionManager{
									HttpFilters: []*hcm.HttpFilter{
										{
											Name:       wellknown.Fault,
											ConfigType: &hcm.HttpFilter_TypedConfig{TypedConfig: faultFilterInAny},
										},
										{Name: "http-filter2"},
										{Name: "http-filter-to-be-removed"},
									},
								}),
							},
						},
					},
				},
				{
					FilterChainMatch: &listener.FilterChainMatch{
						AddressSuffix: "0.0.0.0",
					},
					Filters: []*listener.Filter{
						{Name: "network-filter-should-not-be-replaced"},
					},
				},
				{
					FilterChainMatch: &listener.FilterChainMatch{
						DestinationPort: &wrapperspb.UInt32Value{
							Value: 6380,
						},
					},
					Filters: []*listener.Filter{
						{Name: "network-filter-to-be-replaced"},
					},
				},
				{
					Name: "filter-chain-name-not-match",
					Filters: []*listener.Filter{
						{Name: "custom-network-filter-1"},
						{Name: "custom-network-filter-2"},
					},
				},
				{
					Name: "filter-chain-name-match",
					Filters: []*listener.Filter{
						{Name: "custom-network-filter-1"},
						{Name: "custom-network-filter-2"},
					},
				},
				{
					Name:             "catch-all",
					FilterChainMatch: &listener.FilterChainMatch{},
					Filters: []*listener.Filter{
						{
							Name: wellknown.HTTPConnectionManager,
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: protoconv.MessageToAny(&hcm.HttpConnectionManager{
									HttpFilters: []*hcm.HttpFilter{
										{Name: "base"},
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
			Name:             model.VirtualInboundListenerName,
			UseOriginalDst:   istio_proto.BoolTrue,
			TrafficDirection: core.TrafficDirection_INBOUND,
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
					Name: "virtualInbound-blackhole",
					FilterChainMatch: &listener.FilterChainMatch{
						DestinationPort: &wrapperspb.UInt32Value{
							Value: 15006,
						},
					},
					Filters: []*listener.Filter{
						{
							Name: wellknown.TCPProxy,
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: protoconv.MessageToAny(&tcp_proxy.TcpProxy{
									StatPrefix:       util.BlackHoleCluster,
									ClusterSpecifier: &tcp_proxy.TcpProxy_Cluster{Cluster: util.BlackHoleCluster},
								}),
							},
						},
					},
				},
				{
					FilterChainMatch: &listener.FilterChainMatch{
						DestinationPort: &wrapperspb.UInt32Value{
							Value: 80,
						},
					},
					TransportSocket: &core.TransportSocket{
						Name: "envoy.transport_sockets.tls",
						ConfigType: &core.TransportSocket_TypedConfig{
							TypedConfig: protoconv.MessageToAny(&tls.DownstreamTlsContext{
								CommonTlsContext: &tls.CommonTlsContext{
									AlpnProtocols: []string{"h2-80", "http/1.1-80"},
								},
							}),
						},
					},
					Filters: []*listener.Filter{
						{
							Name: wellknown.HTTPConnectionManager,
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: protoconv.MessageToAny(&hcm.HttpConnectionManager{
									XffNumTrustedHops:            4,
									MergeSlashes:                 true,
									AlwaysSetRequestIdInResponse: true,
									HttpFilters: []*hcm.HttpFilter{
										{Name: "http-filter0"},
										{
											Name:       wellknown.Fault,
											ConfigType: &hcm.HttpFilter_TypedConfig{TypedConfig: faultFilterOutAny},
										},
										{Name: "http-filter3"},
										{Name: "http-filter2"},
										{Name: "http-filter-5"},
										{Name: "http-filter4"},
										{Name: "http-filter-to-be-removed-then-add"},
									},
								}),
							},
						},
					},
				},
				{
					FilterChainMatch: &listener.FilterChainMatch{
						AddressSuffix: "0.0.0.0",
					},
					Filters: []*listener.Filter{
						{Name: "network-filter-should-not-be-replaced"},
					},
				},
				{
					FilterChainMatch: &listener.FilterChainMatch{
						DestinationPort: &wrapperspb.UInt32Value{
							Value: 6380,
						},
					},
					TransportSocket: &core.TransportSocket{
						Name: "envoy.transport_sockets.tls",
						ConfigType: &core.TransportSocket_TypedConfig{
							TypedConfig: protoconv.MessageToAny(&tls.DownstreamTlsContext{
								CommonTlsContext: &tls.CommonTlsContext{
									AlpnProtocols: []string{"h2-6380", "http/1.1-6380"},
								},
							}),
						},
					},
					Filters: []*listener.Filter{
						{
							Name: "envoy.redis_proxy",
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: protoconv.MessageToAny(&redis.RedisProxy{
									StatPrefix: "redis_stats",
									PrefixRoutes: &redis.RedisProxy_PrefixRoutes{
										CatchAllRoute: &redis.RedisProxy_PrefixRoutes_Route{
											Cluster: "custom-redis-cluster",
										},
									},
								}),
							},
						},
					},
				},
				{
					Name: "filter-chain-name-not-match",
					Filters: []*listener.Filter{
						{Name: "custom-network-filter-1"},
						{Name: "custom-network-filter-2"},
					},
				},
				{
					Name: "filter-chain-name-match",
					Filters: []*listener.Filter{
						{Name: "custom-network-filter-1"},
					},
				},
				{
					Name:             "catch-all",
					FilterChainMatch: &listener.FilterChainMatch{},
					Filters: []*listener.Filter{
						{
							Name: wellknown.HTTPConnectionManager,
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: protoconv.MessageToAny(&hcm.HttpConnectionManager{
									HttpFilters: []*hcm.HttpFilter{
										{Name: "base"},
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
			Raw: map[string]any{
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
			Raw: map[string]any{
				"foo": "sidecar",
				"bar": "proxy",
			},
		},
	}
	serviceDiscovery := memregistry.NewServiceDiscovery()
	e := newTestEnvironment(serviceDiscovery, testMesh, buildEnvoyFilterConfigStore(configPatches))
	push := model.NewPushContext()
	push.InitContext(e, nil, nil)

	// Test different priorities
	ep := newTestEnvironment(serviceDiscovery, testMesh, buildEnvoyFilterConfigStoreWithPriorities(configPatchesPriorities, priorities))
	pushPriorities := model.NewPushContext()
	pushPriorities.InitContext(ep, nil, nil)

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
			name: "gateway lds with priority",
			args: args{
				patchContext: networking.EnvoyFilter_GATEWAY,
				proxy:        gatewayProxy,
				push:         pushPriorities,
				listeners:    gatewayPriorityIn,
				skipAdds:     false,
			},
			want: gatewayPriorityOut,
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
			got := ApplyListenerPatches(tt.args.patchContext, tt.args.push.EnvoyFilters(tt.args.proxy),
				tt.args.listeners, tt.args.skipAdds)
			if diff := cmp.Diff(tt.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("ApplyListenerPatches(): %s mismatch (-want +got):\n%s", tt.name, diff)
			}
		})
	}
}
