// Copyright 2018 Istio Authors
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
	"github.com/gogo/protobuf/proto"
	"reflect"
	"strings"
	"testing"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
)

var (
	testMesh = meshconfig.MeshConfig{
		ConnectTimeout: &types.Duration{
			Seconds: 10,
			Nanos:   1,
		},
	}
)

func buildEnvoyFilterConfigStore(configPatches []*networking.EnvoyFilter_EnvoyConfigObjectPatch) *fakes.IstioConfigStore {
	return &fakes.IstioConfigStore{
		ListStub: func(typ, namespace string) (configs []model.Config, e error) {
			if typ == "envoy-filter" {
				configs = []model.Config{
					{
						ConfigMeta: model.ConfigMeta{
							Name:      "test-envoyfilter",
							Namespace: "not-default",
						},
						Spec: &networking.EnvoyFilter{
							ConfigPatches: configPatches,
						},
					},
				}
			}
			return
		},
		EnvoyFilterStub: func(workloadLabels model.LabelsCollection) *model.Config {
			return &model.Config{
				ConfigMeta: model.ConfigMeta{
					Name:      "test-envoyfilter",
					Namespace: "not-default",
				},
				Spec: &networking.EnvoyFilter{
					ConfigPatches: configPatches,
				},
			}
		},
	}
}

func buildPatchStruct(config string) *types.Struct {
	val := &types.Struct{}
	jsonpb.Unmarshal(strings.NewReader(config), val)
	return val
}

func newTestEnvironment(serviceDiscovery model.ServiceDiscovery, mesh meshconfig.MeshConfig,
	configStore model.IstioConfigStore) *model.Environment {
	env := &model.Environment{
		ServiceDiscovery: serviceDiscovery,
		IstioConfigStore: configStore,
		Mesh:             &mesh,
	}

	env.PushContext = model.NewPushContext()
	_ = env.PushContext.InitContext(env)

	return env
}

func TestApplyListenerPatches(t *testing.T) {
	configPatches := []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
		{
			ApplyTo: networking.EnvoyFilter_LISTENER,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name":"new-outbound-listener1"}`),
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
	}

	sidecarOutboundIn := []*xdsapi.Listener{
		{
			Name: "12345",
			Address: core.Address{
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
		},
	}

	sidecarOutboundOut := []*xdsapi.Listener{
		{
			Name: "12345",
			Address: core.Address{
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
		},
		{
			Name: "new-outbound-listener1",
		},
	}

	sidecarInboundIn := []*xdsapi.Listener{
		{
			Name: "12345",
			Address: core.Address{
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
		},
	}

	sidecarInboundOut := []*xdsapi.Listener{
		{
			Name: "another-listener",
		},
	}

	gatewayIn := []*xdsapi.Listener{
		{
			Name: "80",
			Address: core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 80,
						},
					},
				},
			},
			FilterChains: []listener.FilterChain{{Filters: []listener.Filter{{Name: "network-filter"}}}},
		},
		{
			Name: "another-listener",
		},
	}

	gatewayOut := []*xdsapi.Listener{
		{
			Name: "80",
			Address: core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 80,
						},
					},
				},
			},
			ListenerFilters: []listener.ListenerFilter{{Name: "foo"}},
			FilterChains:    []listener.FilterChain{{Filters: []listener.Filter{{Name: "network-filter"}}}},
		},
		{
			Name: "another-listener",
		},
	}

	sidecarProxy := &model.Proxy{Type: model.SidecarProxy, ConfigNamespace: "not-default"}
	gatewayProxy := &model.Proxy{Type: model.Router, ConfigNamespace: "not-default"}
	serviceDiscovery := &fakes.ServiceDiscovery{}
	env := newTestEnvironment(serviceDiscovery, testMesh, buildEnvoyFilterConfigStore(configPatches))
	push := model.NewPushContext()
	push.InitContext(env)

	temp := &xdsapi.Listener{
		ListenerFilters: []listener.ListenerFilter{{Name: "foo"}},
	}
	proto.Merge(gatewayIn[0], temp)
	type args struct {
		patchContext networking.EnvoyFilter_PatchContext
		proxy        *model.Proxy
		push         *model.PushContext
		listeners    []*xdsapi.Listener
		skipAdds     bool
	}
	tests := []struct {
		name string
		args args
		want []*xdsapi.Listener
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
				listeners:    sidecarOutboundIn,
				skipAdds:     true,
			},
			want: sidecarOutboundIn, // should be same
		},
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ApplyListenerPatches(tt.args.patchContext, tt.args.proxy, tt.args.push,
				tt.args.listeners, tt.args.skipAdds); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ApplyListenerPatches() = %v, want %v", got, tt.want)
			}
		})
	}
}
