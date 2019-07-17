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

package v1alpha3

import (
	"net"
	"reflect"
	"strings"
	"testing"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/types"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
	"istio.io/istio/pilot/pkg/networking/plugin"
)

func buildEnvoyFilterConfigStore(configPatches []*networking.EnvoyFilter_EnvoyConfigObjectPatch) *fakes.IstioConfigStore {
	return &fakes.IstioConfigStore{
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

func buildListenerPatches(config string) []*networking.EnvoyFilter_EnvoyConfigObjectPatch {
	val := &types.Struct{}
	jsonpb.Unmarshal(strings.NewReader(config), val)

	return []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
		{
			ApplyTo: networking.EnvoyFilter_LISTENER,
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     val,
			},
		},
	}
}

func buildPatchStruct(config string) *types.Struct {
	val := &types.Struct{}
	jsonpb.Unmarshal(strings.NewReader(config), val)
	return val
}

func TestApplyListenerConfigPatches(t *testing.T) {
	listenerConfig := `{"address": { "pipe": { "path": "some-path" } }, "filter_chains": [{"filters": [{"name": "envoy.ratelimit"}]}]}`
	invalidConfig := `{"address": { "non-existent-field": { "path": "some-path" } }, "filter_chains": [{"filters": [{"name": "envoy.ratelimit"}]}]}`

	testCases := []struct {
		name      string
		listeners []*xdsapi.Listener
		patches   []*networking.EnvoyFilter_EnvoyConfigObjectPatch
		labels    model.LabelsCollection
		result    []*xdsapi.Listener
	}{
		{
			name:      "successfully adds a listener",
			listeners: make([]*xdsapi.Listener, 0),
			patches:   buildListenerPatches(listenerConfig),
			labels:    model.LabelsCollection{},
			result: []*xdsapi.Listener{
				{
					Address: core.Address{
						Address: &core.Address_Pipe{
							Pipe: &core.Pipe{
								Path: "some-path",
							},
						},
					},
					FilterChains: []listener.FilterChain{
						{
							Filters: []listener.Filter{
								{
									Name: "envoy.ratelimit",
								},
							},
						},
					},
				},
			},
		},
		{
			name:      "does not add a listener with invalid config",
			listeners: make([]*xdsapi.Listener, 0),
			patches:   buildListenerPatches(invalidConfig),
			labels:    model.LabelsCollection{},
			result:    []*xdsapi.Listener{},
		},
		{
			name:      "does not merge listener",
			listeners: make([]*xdsapi.Listener, 0),
			patches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_LISTENER,
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_MERGE,
						Value:     buildPatchStruct(listenerConfig),
					},
				},
			},
			labels: model.LabelsCollection{},
			result: []*xdsapi.Listener{},
		},
		{
			name:      "does not add new listener with empty patch",
			listeners: make([]*xdsapi.Listener, 0),
			patches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_LISTENER,
				},
			},
			labels: model.LabelsCollection{},
			result: []*xdsapi.Listener{},
		},
		{
			name: "does not remove listener",
			listeners: []*xdsapi.Listener{
				{
					Address: core.Address{
						Address: &core.Address_Pipe{
							Pipe: &core.Pipe{
								Path: "some-path",
							},
						},
					},
					FilterChains: []listener.FilterChain{
						{
							Filters: []listener.Filter{
								{
									Name: "envoy.ratelimit",
								},
							},
						},
					},
				},
			},
			patches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_LISTENER,
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
						Value:     buildPatchStruct(listenerConfig),
					},
				},
			},
			labels: model.LabelsCollection{},
			result: []*xdsapi.Listener{
				{
					Address: core.Address{
						Address: &core.Address_Pipe{
							Pipe: &core.Pipe{
								Path: "some-path",
							},
						},
					},
					FilterChains: []listener.FilterChain{
						{
							Filters: []listener.Filter{
								{
									Name: "envoy.ratelimit",
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		serviceDiscovery := &fakes.ServiceDiscovery{}
		env := newTestEnvironment(serviceDiscovery, testMesh, buildEnvoyFilterConfigStore(tc.patches))
		builder := NewListenerBuilder(&model.Proxy{Type: model.SidecarProxy})
		builder.inboundListeners = tc.listeners

		ret := applyListenerConfigPatches(builder, env, tc.labels)
		if !reflect.DeepEqual(tc.result, ret) {
			t.Errorf("test case %s: expecting %v but got %v", tc.name, tc.result, ret)
		}
	}
}

func TestApplyClusterConfigPatches(t *testing.T) {
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
	}

	sidecarInput := []*xdsapi.Cluster{
		{Name: "cluster1", DnsLookupFamily: xdsapi.Cluster_V4_ONLY, LbPolicy: xdsapi.Cluster_ROUND_ROBIN},
		{Name: "cluster2",
			Http2ProtocolOptions: &core.Http2ProtocolOptions{
				AllowConnect:  true,
				AllowMetadata: true,
			}, LbPolicy: xdsapi.Cluster_MAGLEV,
		},
		{Name: "inbound|9999||mgmtCluster"},
	}
	sidecarOutput := []*xdsapi.Cluster{
		{Name: "cluster1", DnsLookupFamily: xdsapi.Cluster_V6_ONLY, LbPolicy: xdsapi.Cluster_RING_HASH},
		{Name: "cluster2",
			Http2ProtocolOptions: &core.Http2ProtocolOptions{
				AllowConnect:  true,
				AllowMetadata: true,
			}, LbPolicy: xdsapi.Cluster_RING_HASH, DnsLookupFamily: xdsapi.Cluster_V6_ONLY,
		},
		{Name: "new-cluster1"},
		{Name: "new-cluster2"},
	}

	gatewayInput := []*xdsapi.Cluster{
		{Name: "cluster1", DnsLookupFamily: xdsapi.Cluster_V4_ONLY, LbPolicy: xdsapi.Cluster_ROUND_ROBIN},
		{Name: "cluster2",
			Http2ProtocolOptions: &core.Http2ProtocolOptions{
				AllowConnect:  true,
				AllowMetadata: true,
			}, LbPolicy: xdsapi.Cluster_MAGLEV,
		},
		{Name: "inbound|9999||mgmtCluster"},
		{Name: "outbound|443||gateway.com"},
	}
	gatewayOutput := []*xdsapi.Cluster{
		{Name: "cluster1", DnsLookupFamily: xdsapi.Cluster_V6_ONLY, LbPolicy: xdsapi.Cluster_RING_HASH},
		{Name: "cluster2",
			Http2ProtocolOptions: &core.Http2ProtocolOptions{
				AllowConnect:  true,
				AllowMetadata: true,
			}, LbPolicy: xdsapi.Cluster_RING_HASH, DnsLookupFamily: xdsapi.Cluster_V6_ONLY,
		},
		{Name: "inbound|9999||mgmtCluster", DnsLookupFamily: xdsapi.Cluster_V6_ONLY, LbPolicy: xdsapi.Cluster_RING_HASH},
		{Name: "new-cluster2"},
	}

	testCases := []struct {
		name   string
		input  []*xdsapi.Cluster
		proxy  *model.Proxy
		output []*xdsapi.Cluster
	}{
		{
			name:   "sidecar cds patch",
			input:  sidecarInput,
			proxy:  &model.Proxy{Type: model.SidecarProxy},
			output: sidecarOutput,
		},
		{
			name:   "gateway cds patch",
			input:  gatewayInput,
			proxy:  &model.Proxy{Type: model.Router},
			output: gatewayOutput,
		},
	}

	serviceDiscovery := &fakes.ServiceDiscovery{}
	env := newTestEnvironment(serviceDiscovery, testMesh, buildEnvoyFilterConfigStore(configPatches))

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ret := applyClusterPatches(env, tc.proxy, nil, tc.input)
			if !reflect.DeepEqual(tc.output, ret) {
				t.Errorf("test case %s: expecting %v but got %v", tc.name, tc.output, ret)
			}
		})
	}
}

func TestListenerMatch(t *testing.T) {
	inputParams := &plugin.InputParams{
		ListenerProtocol: plugin.ListenerProtocolHTTP,
		Node: &model.Proxy{
			Type: model.SidecarProxy,
		},
		Port: &model.Port{
			Name: "http-foo",
			Port: 80,
		},
	}

	testCases := []struct {
		name           string
		inputParams    *plugin.InputParams
		listenerIP     net.IP
		matchCondition *networking.EnvoyFilter_DeprecatedListenerMatch
		direction      networking.EnvoyFilter_DeprecatedListenerMatch_ListenerType
		result         bool
	}{
		{
			name:        "empty match",
			inputParams: inputParams,
			result:      true,
		},
		{
			name:           "match by port",
			inputParams:    inputParams,
			matchCondition: &networking.EnvoyFilter_DeprecatedListenerMatch{PortNumber: 80},
			result:         true,
		},
		{
			name:           "match by port name prefix",
			inputParams:    inputParams,
			matchCondition: &networking.EnvoyFilter_DeprecatedListenerMatch{PortNamePrefix: "http"},
			result:         true,
		},
		{
			name:           "match by listener type",
			inputParams:    inputParams,
			direction:      networking.EnvoyFilter_DeprecatedListenerMatch_SIDECAR_OUTBOUND,
			matchCondition: &networking.EnvoyFilter_DeprecatedListenerMatch{ListenerType: networking.EnvoyFilter_DeprecatedListenerMatch_SIDECAR_OUTBOUND},
			result:         true,
		},
		{
			name:           "match by listener protocol",
			inputParams:    inputParams,
			matchCondition: &networking.EnvoyFilter_DeprecatedListenerMatch{ListenerProtocol: networking.EnvoyFilter_DeprecatedListenerMatch_HTTP},
			result:         true,
		},
		{
			name:           "match by listener address with CIDR",
			inputParams:    inputParams,
			listenerIP:     net.ParseIP("10.10.10.10"),
			matchCondition: &networking.EnvoyFilter_DeprecatedListenerMatch{Address: []string{"10.10.10.10/24", "192.168.0.1/24"}},
			result:         true,
		},
		{
			name:        "match outbound sidecar http listeners on 10.10.10.0/24:80, with port name prefix http-*",
			inputParams: inputParams,
			listenerIP:  net.ParseIP("10.10.10.10"),
			direction:   networking.EnvoyFilter_DeprecatedListenerMatch_SIDECAR_OUTBOUND,
			matchCondition: &networking.EnvoyFilter_DeprecatedListenerMatch{
				PortNumber:       80,
				PortNamePrefix:   "http",
				ListenerType:     networking.EnvoyFilter_DeprecatedListenerMatch_SIDECAR_OUTBOUND,
				ListenerProtocol: networking.EnvoyFilter_DeprecatedListenerMatch_HTTP,
				Address:          []string{"10.10.10.0/24"},
			},
			result: true,
		},
		{
			name:        "does not match: outbound sidecar http listeners on 10.10.10.0/24:80, with port name prefix tcp-*",
			inputParams: inputParams,
			listenerIP:  net.ParseIP("10.10.10.10"),
			direction:   networking.EnvoyFilter_DeprecatedListenerMatch_SIDECAR_OUTBOUND,
			matchCondition: &networking.EnvoyFilter_DeprecatedListenerMatch{
				PortNumber:       80,
				PortNamePrefix:   "tcp",
				ListenerType:     networking.EnvoyFilter_DeprecatedListenerMatch_SIDECAR_OUTBOUND,
				ListenerProtocol: networking.EnvoyFilter_DeprecatedListenerMatch_HTTP,
				Address:          []string{"10.10.10.0/24"},
			},
			result: false,
		},
		{
			name:        "does not match: inbound sidecar http listeners with port name prefix http-*",
			inputParams: inputParams,
			direction:   networking.EnvoyFilter_DeprecatedListenerMatch_SIDECAR_OUTBOUND,
			matchCondition: &networking.EnvoyFilter_DeprecatedListenerMatch{
				PortNamePrefix:   "http",
				ListenerType:     networking.EnvoyFilter_DeprecatedListenerMatch_SIDECAR_INBOUND,
				ListenerProtocol: networking.EnvoyFilter_DeprecatedListenerMatch_HTTP,
			},
			result: false,
		},
		{
			name:        "does not match: outbound gateway http listeners on 10.10.10.0/24:80, with port name prefix http-*",
			inputParams: inputParams,
			listenerIP:  net.ParseIP("10.10.10.10"),
			direction:   networking.EnvoyFilter_DeprecatedListenerMatch_SIDECAR_OUTBOUND,
			matchCondition: &networking.EnvoyFilter_DeprecatedListenerMatch{
				PortNumber:       80,
				PortNamePrefix:   "http",
				ListenerType:     networking.EnvoyFilter_DeprecatedListenerMatch_GATEWAY,
				ListenerProtocol: networking.EnvoyFilter_DeprecatedListenerMatch_HTTP,
				Address:          []string{"10.10.10.0/24"},
			},
			result: false,
		},
		{
			name:        "does not match: outbound sidecar listeners on 172.16.0.1/16:80, with port name prefix http-*",
			inputParams: inputParams,
			listenerIP:  net.ParseIP("10.10.10.10"),
			direction:   networking.EnvoyFilter_DeprecatedListenerMatch_SIDECAR_OUTBOUND,
			matchCondition: &networking.EnvoyFilter_DeprecatedListenerMatch{
				PortNumber:       80,
				PortNamePrefix:   "http",
				ListenerType:     networking.EnvoyFilter_DeprecatedListenerMatch_SIDECAR_OUTBOUND,
				ListenerProtocol: networking.EnvoyFilter_DeprecatedListenerMatch_HTTP,
				Address:          []string{"172.16.0.1/16"},
			},
			result: false,
		},
	}

	for _, tc := range testCases {
		tc.inputParams.DeprecatedListenerCategory = tc.direction
		ret := deprecatedListenerMatch(tc.inputParams, tc.listenerIP, tc.matchCondition)
		if tc.result != ret {
			t.Errorf("%s: expecting %v but got %v", tc.name, tc.result, ret)
		}
	}
}

func Test_virtualHostMatch(t *testing.T) {
	type args struct {
		match *networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch
		vh    *route.VirtualHost
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "match & vh is nil",
			args: args{},
			want: true,
		},
		{
			name: "match is nil",
			args: args{
				vh: &route.VirtualHost{
					Name: "scooby",
				},
			},
			want: true,
		},
		{
			name: "vh is nil",
			args: args{
				match: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{},
			},
			want: false,
		},
		{
			name: "full match",
			args: args{
				vh: &route.VirtualHost{
					Name: "scooby",
				},
				match: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
					Name: "scooby",
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := virtualHostMatch(tt.args.match, tt.args.vh); got != tt.want {
				t.Errorf("virtualHostMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_routeConfigurationMatch(t *testing.T) {
	type args struct {
		rc             *xdsapi.RouteConfiguration
		vh             *route.VirtualHost
		pluginParams   *plugin.InputParams
		matchCondition *networking.EnvoyFilter_EnvoyConfigObjectMatch
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "nil match",
			args: args{},
			want: true,
		},
		{
			name: "context mismatch",
			args: args{
				pluginParams: &plugin.InputParams{
					ListenerCategory: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				},
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
					Context: networking.EnvoyFilter_GATEWAY,
				},
			},
			want: false,
		},
		{
			name: "nil route match",
			args: args{
				pluginParams: &plugin.InputParams{
					ListenerCategory: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				},
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
					Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				},
			},
			want: true,
		},
		{
			name: "rc name mismatch",
			args: args{
				pluginParams: &plugin.InputParams{
					ListenerCategory: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				},
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
					Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
					ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
						RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{Name: "scooby.80"},
					},
				},
				rc: &xdsapi.RouteConfiguration{Name: "scooby.90"},
			},
			want: false,
		},
		{
			name: "vh name mismatch",
			args: args{
				pluginParams: &plugin.InputParams{
					ListenerCategory: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				},
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
					Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
					ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
						RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
							Name: "scooby.80",
							Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
								Name: "scoobydoo",
							},
						},
					},
				},
				rc: &xdsapi.RouteConfiguration{Name: "scooby.80"},
				vh: &route.VirtualHost{Name: "scrappy"},
			},
			want: false,
		},
		{
			name: "sidecar port match, vh name match",
			args: args{
				pluginParams: &plugin.InputParams{
					ListenerCategory: networking.EnvoyFilter_SIDECAR_OUTBOUND,
					Port:             &model.Port{Port: 80},
					Node:             &model.Proxy{Type: model.SidecarProxy},
				},
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
					Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
					ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
						RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
							PortNumber: 80,
							Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
								Name: "scoobydoo",
							},
						},
					},
				},
				rc: &xdsapi.RouteConfiguration{Name: "80"},
				vh: &route.VirtualHost{Name: "scoobydoo"},
			},
			want: true,
		},
		{
			name: "sidecar port mismatch",
			args: args{
				pluginParams: &plugin.InputParams{
					ListenerCategory: networking.EnvoyFilter_SIDECAR_OUTBOUND,
					Port:             &model.Port{Port: 80},
					Node:             &model.Proxy{Type: model.SidecarProxy},
				},
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
					Context: networking.EnvoyFilter_ANY,
					ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
						RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
							PortNumber: 90,
							Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
								Name: "scoobydoo",
							},
						},
					},
				},
				rc: &xdsapi.RouteConfiguration{Name: "80"},
				vh: &route.VirtualHost{Name: "scoobydoo"},
			},
			want: false,
		},
		{
			name: "gateway fields match",
			args: args{
				pluginParams: &plugin.InputParams{
					ListenerCategory: networking.EnvoyFilter_GATEWAY,
					Node:             &model.Proxy{Type: model.Router},
				},
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
					Context: networking.EnvoyFilter_GATEWAY,
					ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
						RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
							PortNumber: 443,
							PortName:   "app1",
							Gateway:    "ns1/gw1",
							Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
								Name: "scoobydoo",
							},
						},
					},
				},
				rc: &xdsapi.RouteConfiguration{Name: "https.443.app1.gw1.ns1"},
				vh: &route.VirtualHost{Name: "scoobydoo"},
			},
			want: true,
		},
		{
			name: "gateway fields mismatch",
			args: args{
				pluginParams: &plugin.InputParams{
					ListenerCategory: networking.EnvoyFilter_GATEWAY,
					Node:             &model.Proxy{Type: model.Router},
				},
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
					Context: networking.EnvoyFilter_GATEWAY,
					ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
						RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
							PortNumber: 443,
							PortName:   "app1",
							Gateway:    "ns1/gw1",
							Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
								Name: "scoobydoo",
							},
						},
					},
				},
				rc: &xdsapi.RouteConfiguration{Name: "http.80"},
				vh: &route.VirtualHost{Name: "scoobydoo"},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := routeConfigurationMatch(tt.args.rc, tt.args.vh, tt.args.pluginParams, tt.args.matchCondition); got != tt.want {
				t.Errorf("routeConfigurationMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_clusterMatch(t *testing.T) {
	type args struct {
		proxy          *model.Proxy
		cluster        *xdsapi.Cluster
		matchCondition *networking.EnvoyFilter_EnvoyConfigObjectMatch
		operation      networking.EnvoyFilter_Patch_Operation
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "nil match",
			args: args{},
			want: true,
		},
		{
			name: "add op match with gateway",
			args: args{
				proxy:          &model.Proxy{Type: model.Router},
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{Context: networking.EnvoyFilter_GATEWAY},
				operation:      networking.EnvoyFilter_Patch_ADD,
			},
			want: true,
		},
		{
			name: "add op match with sidecar",
			args: args{
				proxy:          &model.Proxy{Type: model.SidecarProxy},
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{Context: networking.EnvoyFilter_SIDECAR_INBOUND},
				operation:      networking.EnvoyFilter_Patch_ADD,
			},
			want: true,
		},
		{
			name: "add op mismatch with gateway",
			args: args{
				proxy:          &model.Proxy{Type: model.Router},
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{Context: networking.EnvoyFilter_SIDECAR_OUTBOUND},
				operation:      networking.EnvoyFilter_Patch_ADD,
			},
			want: false,
		},
		{
			name: "merge op mismatch with gateway",
			args: args{
				proxy:          &model.Proxy{Type: model.Router},
				cluster:        &xdsapi.Cluster{Name: "somecluster"},
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{Context: networking.EnvoyFilter_SIDECAR_OUTBOUND},
				operation:      networking.EnvoyFilter_Patch_MERGE,
			},
			want: false,
		},
		{
			name: "merge op match with gateway",
			args: args{
				proxy:          &model.Proxy{Type: model.Router},
				cluster:        &xdsapi.Cluster{Name: "somecluster"},
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{Context: networking.EnvoyFilter_GATEWAY},
				operation:      networking.EnvoyFilter_Patch_MERGE,
			},
			want: true,
		},
		{
			name: "merge op match with sidecar inbound",
			args: args{
				proxy:          &model.Proxy{Type: model.SidecarProxy},
				cluster:        &xdsapi.Cluster{Name: "inbound|80|v1|foo.com"},
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{Context: networking.EnvoyFilter_SIDECAR_INBOUND},
				operation:      networking.EnvoyFilter_Patch_MERGE,
			},
			want: true,
		},
		{
			name: "merge op mismatch with sidecar outbound",
			args: args{
				proxy:          &model.Proxy{Type: model.SidecarProxy},
				cluster:        &xdsapi.Cluster{Name: "outbound|80|v1|foo.com"},
				matchCondition: &networking.EnvoyFilter_EnvoyConfigObjectMatch{Context: networking.EnvoyFilter_SIDECAR_INBOUND},
				operation:      networking.EnvoyFilter_Patch_MERGE,
			},
			want: false,
		},
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
				cluster: &xdsapi.Cluster{Name: "scrappy"},
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
				cluster: &xdsapi.Cluster{Name: "outbound|80|v2|foo.bar"},
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
				cluster: &xdsapi.Cluster{Name: "outbound|80|v1|google.com"},
			},
			want: false,
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
				cluster: &xdsapi.Cluster{Name: "outbound|90|v1|foo.bar"},
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
				cluster: &xdsapi.Cluster{Name: "outbound|80|v1|foo.bar"},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clusterMatch(tt.args.proxy, tt.args.cluster, tt.args.matchCondition, tt.args.operation); got != tt.want {
				t.Errorf("clusterMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_applyRouteConfigurationPatches(t *testing.T) {
	configPatches := []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
		{
			ApplyTo: networking.EnvoyFilter_ROUTE_CONFIGURATION,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
					RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
						PortNumber: 80,
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value:     buildPatchStruct(`{"request_headers_to_remove":["h3", "h4"]}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_VIRTUAL_HOST,
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name":"new-vhost"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_VIRTUAL_HOST,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_GATEWAY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
					RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
						Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
							Name: "vhost1",
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{Operation: networking.EnvoyFilter_Patch_REMOVE},
		},
		{
			ApplyTo: networking.EnvoyFilter_VIRTUAL_HOST,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_INBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
					RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
						Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
							Name: "vhost2",
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{Operation: networking.EnvoyFilter_Patch_REMOVE},
		},
		{
			ApplyTo: networking.EnvoyFilter_VIRTUAL_HOST,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_ANY,
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value:     buildPatchStruct(`{"domains":["domain:80"]}`),
			},
		},
	}

	sidecarOutboundRC := &xdsapi.RouteConfiguration{
		Name: "80",
		VirtualHosts: []route.VirtualHost{
			{
				Name:    "foo.com",
				Domains: []string{"domain"},
			},
		},
		RequestHeadersToRemove: []string{"h1", "h2"},
	}
	patchedSidecarOutputRC := &xdsapi.RouteConfiguration{
		Name: "80",
		VirtualHosts: []route.VirtualHost{
			{
				Name:    "foo.com",
				Domains: []string{"domain", "domain:80"},
			},
			{
				Name: "new-vhost",
			},
		},
		RequestHeadersToRemove: []string{"h1", "h2", "h3", "h4"},
	}
	sidecarInboundRC := &xdsapi.RouteConfiguration{
		Name: "inbound|http|80",
		VirtualHosts: []route.VirtualHost{
			{
				Name:    "vhost2",
				Domains: []string{"domain"},
			},
		},
	}
	patchedSidecarInboundRC := &xdsapi.RouteConfiguration{
		Name: "inbound|http|80",
		VirtualHosts: []route.VirtualHost{
			{
				Name: "new-vhost",
			},
		},
	}

	gatewayRC := &xdsapi.RouteConfiguration{
		Name: "80",
		VirtualHosts: []route.VirtualHost{
			{
				Name:    "vhost1",
				Domains: []string{"domain"},
			},
			{
				Name:    "gateway",
				Domains: []string{"gateway"},
			},
		},
	}
	patchedGatewayRC := &xdsapi.RouteConfiguration{
		Name: "80",
		VirtualHosts: []route.VirtualHost{
			{
				Name:    "gateway",
				Domains: []string{"gateway", "domain:80"},
			},
			{
				Name: "new-vhost",
			},
		},
	}

	serviceDiscovery := &fakes.ServiceDiscovery{}
	env := newTestEnvironment(serviceDiscovery, testMesh, buildEnvoyFilterConfigStore(configPatches))

	sidecarOutbundPluginParams := &plugin.InputParams{
		ListenerCategory: networking.EnvoyFilter_SIDECAR_OUTBOUND,
		Env:              env,
		Node:             &model.Proxy{Type: model.SidecarProxy},
		Port:             &model.Port{Port: 80},
	}
	sidecarInboundPluginParams := &plugin.InputParams{
		ListenerCategory: networking.EnvoyFilter_SIDECAR_INBOUND,
		Env:              env,
		Node:             &model.Proxy{Type: model.SidecarProxy},
		Port:             &model.Port{Port: 80},
	}
	gatewayPluginParams := &plugin.InputParams{
		ListenerCategory: networking.EnvoyFilter_GATEWAY,
		Env:              env,
		Node:             &model.Proxy{Type: model.Router},
	}

	type args struct {
		pluginParams       *plugin.InputParams
		routeConfiguration *xdsapi.RouteConfiguration
	}
	tests := []struct {
		name string
		args args
		want *xdsapi.RouteConfiguration
	}{
		{
			name: "sidecar outbound rds patch",
			args: args{
				pluginParams:       sidecarOutbundPluginParams,
				routeConfiguration: sidecarOutboundRC,
			},
			want: patchedSidecarOutputRC,
		},
		{
			name: "sidecar inbound rc patch",
			args: args{
				pluginParams:       sidecarInboundPluginParams,
				routeConfiguration: sidecarInboundRC,
			},
			want: patchedSidecarInboundRC,
		},
		{
			name: "gateway rds patch",
			args: args{
				pluginParams:       gatewayPluginParams,
				routeConfiguration: gatewayRC,
			},
			want: patchedGatewayRC,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := applyRouteConfigurationPatches(tt.args.pluginParams, tt.args.routeConfiguration); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("applyRouteConfigurationPatches() = %v, want %v", got, tt.want)
			}
		})
	}
}
