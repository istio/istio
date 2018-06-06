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

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	envoy_api_v2_route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	hcfilter "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/health_check/v2"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/gogo/protobuf/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
)

func TestBuildHealthCheckFilter(t *testing.T) {
	cases := []struct {
		in       *model.Probe
		expected *http_conn.HttpFilter
	}{
		{
			in: &model.Probe{
				Path: "/health",
			},
			expected: &http_conn.HttpFilter{
				Name: "envoy.health_check",
				Config: util.MessageToStruct(&hcfilter.HealthCheck{
					PassThroughMode: &types.BoolValue{
						Value: true,
					},
					Headers: []*envoy_api_v2_route.HeaderMatcher{
						{
							Name:  ":path",
							Value: "/health",
						},
					},
				}),
			},
		},
	}

	for _, c := range cases {
		if got := BuildHealthCheckFilter(c.in); !reflect.DeepEqual(c.expected, got) {
			t.Errorf("buildHealthCheckFilter(%#v), got:\n%#v\nwanted:\n%#v\n", c.in, got, c.expected)
		}
	}
}

func TestOnInboundListener(t *testing.T) {
	cases := []struct {
		in       *plugin.InputParams
		expected *plugin.MutableObjects
	}{
		{
			in: &plugin.InputParams{
				ListenerType: plugin.ListenerTypeHTTP,
				Node: &model.Proxy{
					Type: model.Sidecar,
				},
				ServiceInstance: &model.ServiceInstance{},
			},
			expected: &plugin.MutableObjects{
				Listener: &xdsapi.Listener{
					FilterChains: []listener.FilterChain{{}},
				},
				FilterChains: []plugin.FilterChain{{
					HTTP: []*http_conn.HttpFilter{},
				}},
			},
		},
		{
			in: &plugin.InputParams{
				ListenerType: plugin.ListenerTypeHTTP,
				Node: &model.Proxy{
					Type: model.Sidecar,
				},
				ServiceInstance: &model.ServiceInstance{
					LivenessProbe: &model.Probe{
						Path: "/alive",
					},
				},
			},
			expected: &plugin.MutableObjects{
				Listener: &xdsapi.Listener{
					FilterChains: []listener.FilterChain{{}},
				},
				FilterChains: []plugin.FilterChain{{
					HTTP: []*http_conn.HttpFilter{
						{
							Name: "envoy.health_check",
							Config: util.MessageToStruct(&hcfilter.HealthCheck{
								PassThroughMode: &types.BoolValue{
									Value: true,
								},
								Headers: []*envoy_api_v2_route.HeaderMatcher{
									{
										Name:  ":path",
										Value: "/alive",
									},
								},
							}),
						},
					},
				}},
			},
		},
		{
			in: &plugin.InputParams{
				ListenerType: plugin.ListenerTypeHTTP,
				Node: &model.Proxy{
					Type: model.Sidecar,
				},
				ServiceInstance: &model.ServiceInstance{
					ReadinessProbe: &model.Probe{
						Path: "/ready",
					},
				},
			},
			expected: &plugin.MutableObjects{
				Listener: &xdsapi.Listener{
					FilterChains: []listener.FilterChain{{}},
				},
				FilterChains: []plugin.FilterChain{{
					HTTP: []*http_conn.HttpFilter{
						{
							Name: "envoy.health_check",
							Config: util.MessageToStruct(&hcfilter.HealthCheck{
								PassThroughMode: &types.BoolValue{
									Value: true,
								},
								Headers: []*envoy_api_v2_route.HeaderMatcher{
									{
										Name:  ":path",
										Value: "/ready",
									},
								},
							}),
						},
					},
				}},
			},
		},
		// Check that a health check filter is created for both readiness and liveness probes
		{
			in: &plugin.InputParams{
				ListenerType: plugin.ListenerTypeHTTP,
				Node: &model.Proxy{
					Type: model.Sidecar,
				},
				ServiceInstance: &model.ServiceInstance{
					ReadinessProbe: &model.Probe{
						Path: "/ready",
					},
					LivenessProbe: &model.Probe{
						Path: "/alive",
					},
				},
			},
			expected: &plugin.MutableObjects{
				Listener: &xdsapi.Listener{
					FilterChains: []listener.FilterChain{{}},
				},
				FilterChains: []plugin.FilterChain{{
					HTTP: []*http_conn.HttpFilter{
						{
							Name: "envoy.health_check",
							Config: util.MessageToStruct(&hcfilter.HealthCheck{
								PassThroughMode: &types.BoolValue{
									Value: true,
								},
								Headers: []*envoy_api_v2_route.HeaderMatcher{
									{
										Name:  ":path",
										Value: "/ready",
									},
								},
							}),
						},
						{
							Name: "envoy.health_check",
							Config: util.MessageToStruct(&hcfilter.HealthCheck{
								PassThroughMode: &types.BoolValue{
									Value: true,
								},
								Headers: []*envoy_api_v2_route.HeaderMatcher{
									{
										Name:  ":path",
										Value: "/alive",
									},
								},
							}),
						},
					},
				}},
			},
		},
		// Check that only one health check filter is created if readiness and liveness probes represent the
		// same endpoint
		{
			in: &plugin.InputParams{
				ListenerType: plugin.ListenerTypeHTTP,
				Node: &model.Proxy{
					Type: model.Sidecar,
				},
				ServiceInstance: &model.ServiceInstance{
					ReadinessProbe: &model.Probe{
						Path: "/health",
					},
					LivenessProbe: &model.Probe{
						Path: "/health",
					},
				},
			},
			expected: &plugin.MutableObjects{
				Listener: &xdsapi.Listener{
					FilterChains: []listener.FilterChain{{}},
				},
				FilterChains: []plugin.FilterChain{{
					HTTP: []*http_conn.HttpFilter{
						{
							Name: "envoy.health_check",
							Config: util.MessageToStruct(&hcfilter.HealthCheck{
								PassThroughMode: &types.BoolValue{
									Value: true,
								},
								Headers: []*envoy_api_v2_route.HeaderMatcher{
									{
										Name:  ":path",
										Value: "/health",
									},
								},
							}),
						},
					},
				}},
			},
		},
	}

	healthPlugin := NewPlugin()
	for _, c := range cases {
		mutable := &plugin.MutableObjects{
			Listener: &xdsapi.Listener{
				FilterChains: []listener.FilterChain{{}},
			},
			FilterChains: []plugin.FilterChain{{
				HTTP: []*http_conn.HttpFilter{},
			}},
		}
		if err := healthPlugin.OnInboundListener(c.in, mutable); err == nil && !reflect.DeepEqual(c.expected, mutable) {
			t.Errorf("TestOnInboundListener(%#v), got:\n%#v\nwanted:\n%#v\n", c.in, mutable, c.expected)
		} else if err != nil {
			t.Fatal("TestOnInboundListener failed")
		}
	}
}
