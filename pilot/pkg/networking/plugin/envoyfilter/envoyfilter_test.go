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
	"net"
	"testing"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
)

func TestListenerMatch(t *testing.T) {
	inputParams := &plugin.InputParams{
		ListenerProtocol: plugin.ListenerProtocolHTTP,
		Node: &model.Proxy{
			Type: model.Sidecar,
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
		direction      string
		matchCondition *networking.EnvoyFilter_ListenerMatch
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
			direction:      OutboundDirection,
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
			direction:   OutboundDirection,
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
			direction:   OutboundDirection,
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
			direction:   OutboundDirection,
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
			direction:   OutboundDirection,
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
			direction:   OutboundDirection,
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
		ret := listenerMatch(tc.inputParams, tc.direction, tc.listenerIP, tc.matchCondition)
		if tc.result != ret {
			t.Errorf("%s: expecting %v but got %v", tc.name, tc.result, ret)
		}
	}
}
