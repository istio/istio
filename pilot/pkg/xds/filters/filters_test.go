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
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	sfsvalue "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/common/set_filter_state/v3"
	router "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	sfs "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/set_filter_state/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/wellknown"
)

func TestBuildRouterFilter(t *testing.T) {
	tests := []struct {
		name     string
		ctx      RouterFilterContext
		expected *hcm.HttpFilter
	}{
		{
			name: "test for build router filter",
			ctx:  RouterFilterContext{StartChildSpan: true},
			expected: &hcm.HttpFilter{
				Name: wellknown.Router,
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: protoconv.MessageToAny(&router.Router{
						StartChildSpan: true,
					}),
				},
			},
		},
		{
			name: "both true",
			ctx: RouterFilterContext{
				StartChildSpan:       true,
				SuppressDebugHeaders: true,
			},
			expected: &hcm.HttpFilter{
				Name: wellknown.Router,
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: protoconv.MessageToAny(&router.Router{
						StartChildSpan:       true,
						SuppressEnvoyHeaders: true,
					}),
				},
			},
		},
		{
			name: "test for build router filter with start child span false",
			ctx:  RouterFilterContext{StartChildSpan: false},
			expected: &hcm.HttpFilter{
				Name: wellknown.Router,
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: protoconv.MessageToAny(&router.Router{
						StartChildSpan: false,
					}),
				},
			},
		},
	}

	for _, tt := range tests {
		result := BuildRouterFilter(tt.ctx)
		if !reflect.DeepEqual(result, tt.expected) {
			t.Errorf("Test %s failed, expected: %v ,got: %v", tt.name, spew.Sdump(result), spew.Sdump(tt.expected))
		}
	}
}

func TestMxFilters(t *testing.T) {
	assert.Equal(t, TCPClusterMx, &cluster.Filter{
		Name: MxFilterName,
		TypedConfig: protoconv.TypedStructWithFields(MetadataExchangeTypeURL, map[string]any{
			"protocol":         "istio-peer-exchange",
			"enable_discovery": true,
		}),
	})

	assert.Equal(t, TCPListenerMx, &listener.Filter{
		Name: MxFilterName,
		ConfigType: &listener.Filter_TypedConfig{
			TypedConfig: protoconv.TypedStructWithFields(MetadataExchangeTypeURL, map[string]any{
				"protocol":         "istio-peer-exchange",
				"enable_discovery": true,
			}),
		},
	})

	assert.Equal(t, SidecarInboundMetadataFilter, &hcm.HttpFilter{
		Name: MxFilterName,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: protoconv.TypedStructWithFields(PeerMetadataTypeURL, map[string]any{
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
	})

	assert.Equal(t, SidecarOutboundMetadataFilter, &hcm.HttpFilter{
		Name: MxFilterName,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: protoconv.TypedStructWithFields(PeerMetadataTypeURL, map[string]any{
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
	})

	assert.Equal(t, SidecarOutboundMetadataFilterSkipHeaders, &hcm.HttpFilter{
		Name: MxFilterName,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: protoconv.TypedStructWithFields(PeerMetadataTypeURL, map[string]any{
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
	})
}

func TestConnectAuthorityFilterPeerDirectRemoteIP(t *testing.T) {
	for name, f := range map[string]*hcm.HttpFilter{
		"ConnectAuthorityFilter":          ConnectAuthorityFilter,
		"ConnectAuthorityFilterPre1_29_2": ConnectAuthorityFilterPre1_29_2,
	} {
		t.Run(name, func(t *testing.T) {
			cfg := &sfs.Config{}
			if err := f.GetTypedConfig().UnmarshalTo(cfg); err != nil {
				t.Fatal(err)
			}
			var found *sfsvalue.FilterStateValue
			for _, v := range cfg.GetOnRequestHeaders() {
				if v.GetObjectKey() == util.PeerDirectRemoteIPFilterStateKey {
					found = v
				}
			}
			if found == nil {
				t.Fatalf("filter state value %q not found", util.PeerDirectRemoteIPFilterStateKey)
			}
			// The value must be the direct peer, not the remote address that headers on the CONNECT request can
			// change, and must be created by a factory of hashable addresses, so that internal connections are not
			// shared between HBONE connections with different peers.
			assert.Equal(t, found.GetFactoryKey(), "envoy.filters.listener.original_dst.remote_ip")
			assert.Equal(t, found.GetFormatString().GetTextFormatSource().GetInlineString(), "%DOWNSTREAM_DIRECT_REMOTE_ADDRESS%")
			assert.Equal(t, found.GetSharedWithUpstream(), sfsvalue.FilterStateValue_ONCE)
		})
	}
}
