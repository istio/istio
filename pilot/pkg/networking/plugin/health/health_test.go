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

	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcfilter "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/health_check/v3"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/proto"
)

func TestBuildHealthCheckFilters(t *testing.T) {
	cases := []struct {
		probes   model.ProbeList
		endpoint *model.IstioEndpoint
		expected networking.FilterChain
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
			endpoint: &model.IstioEndpoint{
				EndpointPort: 8080,
			},
			expected: networking.FilterChain{
				HTTP: []*http_conn.HttpFilter{
					{
						Name: "envoy.health_check",
						ConfigType: &http_conn.HttpFilter_TypedConfig{
							TypedConfig: util.MessageToAny(&hcfilter.HealthCheck{
								PassThroughMode: proto.BoolTrue,
								Headers: []*route.HeaderMatcher{
									{
										Name:                 ":path",
										HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{ExactMatch: "/health"},
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
			endpoint: &model.IstioEndpoint{
				EndpointPort: 8080,
			},
			expected: networking.FilterChain{
				HTTP: []*http_conn.HttpFilter{
					{
						Name: "envoy.health_check",
						ConfigType: &http_conn.HttpFilter_TypedConfig{
							TypedConfig: util.MessageToAny(&hcfilter.HealthCheck{
								PassThroughMode: proto.BoolTrue,
								Headers: []*route.HeaderMatcher{
									{
										Name:                 ":path",
										HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{ExactMatch: "/health"},
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
			endpoint: &model.IstioEndpoint{
				EndpointPort: 8080,
			},
			expected: networking.FilterChain{
				HTTP: []*http_conn.HttpFilter{
					{
						Name: "envoy.health_check",
						ConfigType: &http_conn.HttpFilter_TypedConfig{
							TypedConfig: util.MessageToAny(&hcfilter.HealthCheck{
								PassThroughMode: proto.BoolTrue,
								Headers: []*route.HeaderMatcher{
									{
										Name:                 ":path",
										HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{ExactMatch: "/ready"},
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
								Headers: []*route.HeaderMatcher{
									{
										Name:                 ":path",
										HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{ExactMatch: "/live"},
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
			endpoint: &model.IstioEndpoint{
				EndpointPort: 8080,
			},
			expected: networking.FilterChain{
				HTTP: []*http_conn.HttpFilter{
					{
						Name: "envoy.health_check",
						ConfigType: &http_conn.HttpFilter_TypedConfig{
							TypedConfig: util.MessageToAny(&hcfilter.HealthCheck{
								PassThroughMode: proto.BoolTrue,
								Headers: []*route.HeaderMatcher{
									{
										Name:                 ":path",
										HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{ExactMatch: "/health"},
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
			endpoint: &model.IstioEndpoint{
				EndpointPort: 8080,
			},
			expected: networking.FilterChain{},
		},
	}

	for _, c := range cases {
		var filterChain networking.FilterChain
		buildHealthCheckFilters(&filterChain, c.probes, c.endpoint)
		if !reflect.DeepEqual(c.expected, filterChain) {
			t.Errorf("buildHealthCheckFilters(%#v on endpoint %#v), got:\n%#v\nwanted:\n%#v\n", c.probes, c.endpoint, filterChain, c.expected)
		}
	}
}
