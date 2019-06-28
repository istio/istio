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

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
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
		tc.inputParams.ListenerCategory = tc.direction
		ret := listenerMatch(tc.inputParams, tc.listenerIP, tc.matchCondition)
		if tc.result != ret {
			t.Errorf("%s: expecting %v but got %v", tc.name, tc.result, ret)
		}
	}
}

func TestUpdateFilter(t *testing.T) {
	testCases := []struct {
		name         string
		inputFilters []string
		expected     []string
		envoyFilter  *networking.EnvoyFilter_Filter
	}{
		{
			name: "delete before insert HTTPFilters",
			envoyFilter: &networking.EnvoyFilter_Filter{
				InsertPosition: &networking.EnvoyFilter_InsertPosition{
					Index: networking.EnvoyFilter_InsertPosition_FIRST,
				},
				FilterType: networking.EnvoyFilter_Filter_HTTP,
				FilterName: "envoy.router",
			},
			inputFilters: []string{"envoy.cors", "envoy.fault", "envoy.router"},
			expected:     []string{"envoy.router", "envoy.cors", "envoy.fault"},
		},
		{
			name: "delete before insert networkFilters",
			envoyFilter: &networking.EnvoyFilter_Filter{
				InsertPosition: &networking.EnvoyFilter_InsertPosition{
					Index: networking.EnvoyFilter_InsertPosition_FIRST,
				},
				FilterType: networking.EnvoyFilter_Filter_NETWORK,
				FilterName: "envoy.tcp_proxy",
			},
			inputFilters: []string{"envoy.ratelimit", "envoy.tcp_proxy"},
			expected:     []string{"envoy.tcp_proxy", "envoy.ratelimit"},
		},
		{
			name: "insert HTTPFilters",
			envoyFilter: &networking.EnvoyFilter_Filter{
				InsertPosition: &networking.EnvoyFilter_InsertPosition{
					Index: networking.EnvoyFilter_InsertPosition_LAST,
				},
				FilterType: networking.EnvoyFilter_Filter_HTTP,
				FilterName: "envoy.add",
			},
			inputFilters: []string{"envoy.cors", "envoy.fault"},
			expected:     []string{"envoy.cors", "envoy.fault", "envoy.add"},
		},
		{
			name: "insert networkFilters",
			envoyFilter: &networking.EnvoyFilter_Filter{
				InsertPosition: &networking.EnvoyFilter_InsertPosition{
					Index: networking.EnvoyFilter_InsertPosition_LAST,
				},
				FilterType: networking.EnvoyFilter_Filter_NETWORK,
				FilterName: "envoy.add",
			},
			inputFilters: []string{"envoy.http_connection_manager"},
			expected:     []string{"envoy.http_connection_manager", "envoy.add"},
		},
	}

	for _, tc := range testCases {
		ret := make([]string, 0)
		if tc.envoyFilter.FilterType == networking.EnvoyFilter_Filter_HTTP {
			HTTPFilters := make([]*http_conn.HttpFilter, 0)
			for _, in := range tc.inputFilters {
				HTTPFilters = append(HTTPFilters, &http_conn.HttpFilter{
					Name: in,
				})
			}
			filterChain := &listener.FilterChain{
				Filters: []listener.Filter{
					{
						Name: "envoy.http_connection_manager",
					},
				},
			}
			hcm := &http_conn.HttpConnectionManager{
				HttpFilters: HTTPFilters,
			}
			updateHTTPFilter("fake", filterChain, hcm, tc.envoyFilter, false)
			for _, filter := range hcm.GetHttpFilters() {
				ret = append(ret, filter.Name)
			}
		} else {
			networkFilters := make([]listener.Filter, 0)
			for _, in := range tc.inputFilters {
				networkFilters = append(networkFilters, listener.Filter{
					Name: in,
				})
			}
			filterChain := &listener.FilterChain{
				Filters: networkFilters,
			}
			updateNetworkFilter("fake", filterChain, tc.envoyFilter)
			for _, filter := range filterChain.Filters {
				ret = append(ret, filter.Name)
			}
		}

		if !reflect.DeepEqual(ret, tc.expected) {
			t.Errorf("%s: expecting %v, but got %v", tc.name, tc.expected, ret)
		}
	}
}
