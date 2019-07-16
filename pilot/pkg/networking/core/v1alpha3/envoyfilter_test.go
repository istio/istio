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

	"github.com/gogo/protobuf/jsonpb"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	listener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
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

func buildClusterPatches(config string) []*networking.EnvoyFilter_EnvoyConfigObjectPatch {
	val := &types.Struct{}
	jsonpb.Unmarshal(strings.NewReader(config), val)
	return []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
		{
			ApplyTo: networking.EnvoyFilter_CLUSTER,
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     val,
			},
		},
	}
}

func TestApplyClusterConfigPatches(t *testing.T) {
	clusterConfig := `{"name":"some-cluster"}`

	testCases := []struct {
		name     string
		clusters []*xdsapi.Cluster
		patches  []*networking.EnvoyFilter_EnvoyConfigObjectPatch
		labels   model.LabelsCollection
		result   []*xdsapi.Cluster
	}{
		{
			name:     "successfully adds a cluster",
			clusters: make([]*xdsapi.Cluster, 0),
			patches:  buildClusterPatches(clusterConfig),
			labels:   model.LabelsCollection{},
			result: []*xdsapi.Cluster{
				{
					Name: "some-cluster",
				},
			},
		},
	}

	for _, tc := range testCases {
		serviceDiscovery := &fakes.ServiceDiscovery{}
		env := newTestEnvironment(serviceDiscovery, testMesh, buildEnvoyFilterConfigStore(tc.patches))

		ret := applyClusterConfigPatches(tc.clusters, env, tc.labels)
		if !reflect.DeepEqual(tc.result, ret) {
			t.Errorf("test case %s: expecting %v but got %v", tc.name, tc.result, ret)
		}
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
		ret := listenerMatch(tc.inputParams, tc.listenerIP, tc.matchCondition)
		if tc.result != ret {
			t.Errorf("%s: expecting %v but got %v", tc.name, tc.result, ret)
		}
	}
}
