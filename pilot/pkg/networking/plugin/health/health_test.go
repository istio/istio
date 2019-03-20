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

package health

import (
	"reflect"
	"testing"

	envoy_api_v2_route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	hcfilter "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/health_check/v2"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/proto"
)

func TestBuildHealthCheckFilters(t *testing.T) {
	cases := []struct {
		probes   model.ProbeList
		endpoint *model.NetworkEndpoint
		expected plugin.FilterChain
	}{
		{
			probes: model.ProbeList{
				&model.Probe{
					Port: &model.Port{
						Port: 8080,
					},
					Path: "/health",
				},
			},
			endpoint: &model.NetworkEndpoint{
				Port: 8080,
			},
			expected: plugin.FilterChain{
				HTTP: []*http_conn.HttpFilter{
					{
						Name: "envoy.health_check",
						ConfigType: &http_conn.HttpFilter_TypedConfig{
							TypedConfig: util.MessageToAny(&hcfilter.HealthCheck{
								PassThroughMode: proto.BoolTrue,
								Headers: []*envoy_api_v2_route.HeaderMatcher{
									{
										Name:                 ":path",
										HeaderMatchSpecifier: &envoy_api_v2_route.HeaderMatcher_ExactMatch{ExactMatch: "/health"},
									},
								},
							}),
						},
					},
				},
			},
		},
		// No probe port, so filter should be created
		{
			probes: model.ProbeList{
				&model.Probe{
					Path: "/health",
				},
			},
			endpoint: &model.NetworkEndpoint{
				Port: 8080,
			},
			expected: plugin.FilterChain{
				HTTP: []*http_conn.HttpFilter{
					{
						Name: "envoy.health_check",
						ConfigType: &http_conn.HttpFilter_TypedConfig{
							TypedConfig: util.MessageToAny(&hcfilter.HealthCheck{
								PassThroughMode: proto.BoolTrue,
								Headers: []*envoy_api_v2_route.HeaderMatcher{
									{
										Name:                 ":path",
										HeaderMatchSpecifier: &envoy_api_v2_route.HeaderMatcher_ExactMatch{ExactMatch: "/health"},
									},
								},
							}),
						},
					},
				},
			},
		},
		{
			probes: model.ProbeList{
				&model.Probe{
					Port: &model.Port{
						Port: 8080,
					},
					Path: "/ready",
				},
				&model.Probe{
					Port: &model.Port{
						Port: 8080,
					},
					Path: "/live",
				},
			},
			endpoint: &model.NetworkEndpoint{
				Port: 8080,
			},
			expected: plugin.FilterChain{
				HTTP: []*http_conn.HttpFilter{
					{
						Name: "envoy.health_check",
						ConfigType: &http_conn.HttpFilter_TypedConfig{
							TypedConfig: util.MessageToAny(&hcfilter.HealthCheck{
								PassThroughMode: proto.BoolTrue,
								Headers: []*envoy_api_v2_route.HeaderMatcher{
									{
										Name:                 ":path",
										HeaderMatchSpecifier: &envoy_api_v2_route.HeaderMatcher_ExactMatch{ExactMatch: "/ready"},
									},
								},
							}),
						},
					},
					{
						Name: "envoy.health_check",
						ConfigType: &http_conn.HttpFilter_TypedConfig{
							TypedConfig: util.MessageToAny(&hcfilter.HealthCheck{
								PassThroughMode: proto.BoolTrue,
								Headers: []*envoy_api_v2_route.HeaderMatcher{
									{
										Name:                 ":path",
										HeaderMatchSpecifier: &envoy_api_v2_route.HeaderMatcher_ExactMatch{ExactMatch: "/live"},
									},
								},
							}),
						},
					},
				},
			},
		},
		// Duplicate probes
		{
			probes: model.ProbeList{
				&model.Probe{
					Port: &model.Port{
						Port: 8080,
					},
					Path: "/health",
				},
				&model.Probe{
					Port: &model.Port{
						Port: 8080,
					},
					Path: "/health",
				},
			},
			endpoint: &model.NetworkEndpoint{
				Port: 8080,
			},
			expected: plugin.FilterChain{
				HTTP: []*http_conn.HttpFilter{
					{
						Name: "envoy.health_check",
						ConfigType: &http_conn.HttpFilter_TypedConfig{
							TypedConfig: util.MessageToAny(&hcfilter.HealthCheck{
								PassThroughMode: proto.BoolTrue,
								Headers: []*envoy_api_v2_route.HeaderMatcher{
									{
										Name:                 ":path",
										HeaderMatchSpecifier: &envoy_api_v2_route.HeaderMatcher_ExactMatch{ExactMatch: "/health"},
									},
								},
							}),
						},
					},
				},
			},
		},
		// Probe on management port, so no health check filter required
		{
			probes: model.ProbeList{
				&model.Probe{
					Port: &model.Port{
						Port: 9090,
					},
					Path: "/health",
				},
			},
			endpoint: &model.NetworkEndpoint{
				Port: 8080,
			},
			expected: plugin.FilterChain{},
		},
	}

	for _, c := range cases {
		var filterChain plugin.FilterChain
		buildHealthCheckFilters(&filterChain, c.probes, c.endpoint, true)
		if !reflect.DeepEqual(c.expected, filterChain) {
			t.Errorf("buildHealthCheckFilters(%#v on endpoint %#v), got:\n%#v\nwanted:\n%#v\n", c.probes, c.endpoint, filterChain, c.expected)
		}
	}
}
