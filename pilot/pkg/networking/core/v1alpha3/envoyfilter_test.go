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
	"testing"
	"time"

	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
)

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
		matchCondition *networking.EnvoyFilter_ListenerMatch
		direction      networking.EnvoyFilter_ListenerMatch_ListenerType
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
			matchCondition: &networking.EnvoyFilter_ListenerMatch{PortNumber: 80},
			result:         true,
		},
		{
			name:           "match by port name prefix",
			inputParams:    inputParams,
			matchCondition: &networking.EnvoyFilter_ListenerMatch{PortNamePrefix: "http"},
			result:         true,
		},
		{
			name:           "match by listener type",
			inputParams:    inputParams,
			direction:      networking.EnvoyFilter_ListenerMatch_SIDECAR_OUTBOUND,
			matchCondition: &networking.EnvoyFilter_ListenerMatch{ListenerType: networking.EnvoyFilter_ListenerMatch_SIDECAR_OUTBOUND},
			result:         true,
		},
		{
			name:           "match by listener protocol",
			inputParams:    inputParams,
			matchCondition: &networking.EnvoyFilter_ListenerMatch{ListenerProtocol: networking.EnvoyFilter_ListenerMatch_HTTP},
			result:         true,
		},
		{
			name:           "match by listener address with CIDR",
			inputParams:    inputParams,
			listenerIP:     net.ParseIP("10.10.10.10"),
			matchCondition: &networking.EnvoyFilter_ListenerMatch{Address: []string{"10.10.10.10/24", "192.168.0.1/24"}},
			result:         true,
		},
		{
			name:        "match outbound sidecar http listeners on 10.10.10.0/24:80, with port name prefix http-*",
			inputParams: inputParams,
			listenerIP:  net.ParseIP("10.10.10.10"),
			direction:   networking.EnvoyFilter_ListenerMatch_SIDECAR_OUTBOUND,
			matchCondition: &networking.EnvoyFilter_ListenerMatch{
				PortNumber:       80,
				PortNamePrefix:   "http",
				ListenerType:     networking.EnvoyFilter_ListenerMatch_SIDECAR_OUTBOUND,
				ListenerProtocol: networking.EnvoyFilter_ListenerMatch_HTTP,
				Address:          []string{"10.10.10.0/24"},
			},
			result: true,
		},
		{
			name:        "does not match: outbound sidecar http listeners on 10.10.10.0/24:80, with port name prefix tcp-*",
			inputParams: inputParams,
			listenerIP:  net.ParseIP("10.10.10.10"),
			direction:   networking.EnvoyFilter_ListenerMatch_SIDECAR_OUTBOUND,
			matchCondition: &networking.EnvoyFilter_ListenerMatch{
				PortNumber:       80,
				PortNamePrefix:   "tcp",
				ListenerType:     networking.EnvoyFilter_ListenerMatch_SIDECAR_OUTBOUND,
				ListenerProtocol: networking.EnvoyFilter_ListenerMatch_HTTP,
				Address:          []string{"10.10.10.0/24"},
			},
			result: false,
		},
		{
			name:        "does not match: inbound sidecar http listeners with port name prefix http-*",
			inputParams: inputParams,
			direction:   networking.EnvoyFilter_ListenerMatch_SIDECAR_OUTBOUND,
			matchCondition: &networking.EnvoyFilter_ListenerMatch{
				PortNamePrefix:   "http",
				ListenerType:     networking.EnvoyFilter_ListenerMatch_SIDECAR_INBOUND,
				ListenerProtocol: networking.EnvoyFilter_ListenerMatch_HTTP,
			},
			result: false,
		},
		{
			name:        "does not match: outbound gateway http listeners on 10.10.10.0/24:80, with port name prefix http-*",
			inputParams: inputParams,
			listenerIP:  net.ParseIP("10.10.10.10"),
			direction:   networking.EnvoyFilter_ListenerMatch_SIDECAR_OUTBOUND,
			matchCondition: &networking.EnvoyFilter_ListenerMatch{
				PortNumber:       80,
				PortNamePrefix:   "http",
				ListenerType:     networking.EnvoyFilter_ListenerMatch_GATEWAY,
				ListenerProtocol: networking.EnvoyFilter_ListenerMatch_HTTP,
				Address:          []string{"10.10.10.0/24"},
			},
			result: false,
		},
		{
			name:        "does not match: outbound sidecar listeners on 172.16.0.1/16:80, with port name prefix http-*",
			inputParams: inputParams,
			listenerIP:  net.ParseIP("10.10.10.10"),
			direction:   networking.EnvoyFilter_ListenerMatch_SIDECAR_OUTBOUND,
			matchCondition: &networking.EnvoyFilter_ListenerMatch{
				PortNumber:       80,
				PortNamePrefix:   "http",
				ListenerType:     networking.EnvoyFilter_ListenerMatch_SIDECAR_OUTBOUND,
				ListenerProtocol: networking.EnvoyFilter_ListenerMatch_HTTP,
				Address:          []string{"172.16.0.1/16"},
			},
			result: false,
		},
	}

	for _, tc := range testCases {
		tc.inputParams.ListenerCategory = tc.direction
		ret := listenerMatch(tc.inputParams, tc.listenerIP, tc.matchCondition)
		if tc.result != ret {
			t.Errorf("%s: expecting %v but got %v", tc.name, tc.result, ret)
		}
	}
}

func TestMergeAnyWithStruct(t *testing.T) {
	proxy := &model.Proxy{
		Type:        model.Router,
		IPAddresses: []string{"1.1.1.1"},
		ID:          "v0.default",
		DNSDomain:   "default.example.org",
		Metadata: map[string]string{
			model.NodeMetadataConfigNamespace: "not-default",
			"ISTIO_PROXY_VERSION":             "1.1",
		},
		ConfigNamespace: "not-default",
	}
	mesh := meshconfig.MeshConfig{
		EnableTracing:     true,
		AccessLogFile:     "foo-bar.log",
		AccessLogEncoding: meshconfig.MeshConfig_JSON,
	}
	serviceDiscovery := &fakes.ServiceDiscovery{}
	env := newTestEnvironment(serviceDiscovery, mesh)

	httpOpts := &httpListenerOpts{
		routeConfig:      nil,
		rds:              "8080",
		statPrefix:       "8080",
		addGRPCWebFilter: true,
		useRemoteAddress: false,
	}

	inHCM := buildHTTPConnectionManager(proxy, env, httpOpts, nil)
	inAny := util.MessageToAny(inHCM)

	// listener.go sets this to 0
	newTimeout := 5 * time.Minute
	userHCM := &http_conn.HttpConnectionManager{
		AddUserAgent:      &types.BoolValue{Value: true},
		IdleTimeout:       &newTimeout,
		StreamIdleTimeout: &newTimeout,
		UseRemoteAddress:  &types.BoolValue{Value: true},
		XffNumTrustedHops: 5,
		HttpFilters: []*http_conn.HttpFilter{
			{
				Name: "some filter",
			},
		},
	}

	expectedHCM := proto.Clone(inHCM).(*http_conn.HttpConnectionManager)
	expectedHCM.AddUserAgent = userHCM.AddUserAgent
	expectedHCM.IdleTimeout = userHCM.IdleTimeout
	expectedHCM.StreamIdleTimeout = userHCM.StreamIdleTimeout
	expectedHCM.UseRemoteAddress = userHCM.UseRemoteAddress
	expectedHCM.XffNumTrustedHops = userHCM.XffNumTrustedHops
	expectedHCM.HttpFilters = append(expectedHCM.HttpFilters, userHCM.HttpFilters...)

	envoyFilter := &networking.EnvoyFilter_Filter{
		Op:           &networking.EnvoyFilter_Filter_Merge{Merge: true},
		FilterName:   xdsutil.HTTPConnectionManager,
		FilterConfig: util.MessageToStruct(userHCM),
	}

	outAny, err := mergeAnyWithStruct("somename", inAny, envoyFilter)
	if err != nil {
		t.Errorf("Failed to merge: %v", err)
	}

	outHCM := http_conn.HttpConnectionManager{}
	if err = types.UnmarshalAny(outAny, &outHCM); err != nil {
		t.Errorf("Failed to unmarshall outAny to outHCM: %v", err)
	}

	if !reflect.DeepEqual(expectedHCM, &outHCM) {
		t.Errorf("Merged HCM does not match the expected output")
	}
}
