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

package envoyfilter

import (
	"testing"

	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
)

func Test_virtualHostMatch(t *testing.T) {
	type args struct {
		cp *model.EnvoyFilterConfigPatchWrapper
		vh *route.VirtualHost
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "vh is nil",
			args: args{
				cp: &model.EnvoyFilterConfigPatchWrapper{
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
							RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
								Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{},
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "full match",
			args: args{
				vh: &route.VirtualHost{
					Name: "scooby",
				},
				cp: &model.EnvoyFilterConfigPatchWrapper{
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
							RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
								Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
									Name: "scooby",
								},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "mismatch",
			args: args{
				vh: &route.VirtualHost{
					Name: "scoobydoo",
				},
				cp: &model.EnvoyFilterConfigPatchWrapper{
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
							RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
								Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
									Name: "scooby",
								},
							},
						},
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := virtualHostMatch(tt.args.vh, tt.args.cp); got != tt.want {
				t.Errorf("virtualHostMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_routeConfigurationMatch(t *testing.T) {
	type args struct {
		rc           *route.RouteConfiguration
		patchContext networking.EnvoyFilter_PatchContext
		cp           *model.EnvoyFilterConfigPatchWrapper
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "nil route match",
			args: args{
				patchContext: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				cp: &model.EnvoyFilterConfigPatchWrapper{
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{},
				},
			},
			want: true,
		},
		{
			name: "rc name mismatch",
			args: args{
				patchContext: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				cp: &model.EnvoyFilterConfigPatchWrapper{
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
							RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{Name: "scooby.80"},
						},
					},
				},
				rc: &route.RouteConfiguration{Name: "scooby.90"},
			},
			want: false,
		},
		{
			name: "sidecar port match",
			args: args{
				patchContext: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				cp: &model.EnvoyFilterConfigPatchWrapper{
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
							RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{PortNumber: 80},
						},
					},
				},
				rc: &route.RouteConfiguration{Name: "80"},
			},
			want: true,
		},
		{
			name: "sidecar port mismatch",
			args: args{
				patchContext: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				cp: &model.EnvoyFilterConfigPatchWrapper{
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
							RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{PortNumber: 80},
						},
					},
				},
				rc: &route.RouteConfiguration{Name: "90"},
			},
			want: false,
		},
		{
			name: "gateway fields match",
			args: args{
				patchContext: networking.EnvoyFilter_GATEWAY,
				cp: &model.EnvoyFilterConfigPatchWrapper{
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
							RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
								PortNumber: 443,
								PortName:   "app1",
								Gateway:    "ns1/gw1",
							},
						},
					},
				},
				rc: &route.RouteConfiguration{Name: "https.443.app1.gw1.ns1"},
			},
			want: true,
		},
		{
			name: "gateway fields mismatch",
			args: args{
				patchContext: networking.EnvoyFilter_GATEWAY,
				cp: &model.EnvoyFilterConfigPatchWrapper{
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
							RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
								PortNumber: 443,
								PortName:   "app1",
								Gateway:    "ns1/gw1",
							},
						},
					},
				},
				rc: &route.RouteConfiguration{Name: "http.80"},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := routeConfigurationMatch(tt.args.patchContext, tt.args.rc, tt.args.cp); got != tt.want {
				t.Errorf("routeConfigurationMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyRouteConfigurationPatches(t *testing.T) {
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
			ApplyTo: networking.EnvoyFilter_HTTP_ROUTE,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
					RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
						PortNumber: 80,
						Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
							Route: &networking.EnvoyFilter_RouteConfigurationMatch_RouteMatch{
								Action: networking.EnvoyFilter_RouteConfigurationMatch_RouteMatch_ROUTE,
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_MERGE,
				Value:     buildPatchStruct(`{"route": { "prefix_rewrite": "/foo"}}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_HTTP_ROUTE,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
					RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
						PortNumber: 80,
						Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
							Route: &networking.EnvoyFilter_RouteConfigurationMatch_RouteMatch{
								Name: "bar",
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REMOVE,
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

	sidecarOutboundRC := &route.RouteConfiguration{
		Name: "80",
		VirtualHosts: []*route.VirtualHost{
			{
				Name:    "foo.com",
				Domains: []string{"domain"},
				Routes: []*route.Route{
					{
						Name: "foo",
						Action: &route.Route_Route{
							Route: &route.RouteAction{
								PrefixRewrite: "/",
							},
						},
					},
					{
						Name: "bar",
						Action: &route.Route_Redirect{
							Redirect: &route.RedirectAction{
								ResponseCode: 301,
							},
						},
					},
				},
			},
		},
		RequestHeadersToRemove: []string{"h1", "h2"},
	}
	patchedSidecarOutputRC := &route.RouteConfiguration{
		Name: "80",
		VirtualHosts: []*route.VirtualHost{
			{
				Name:    "foo.com",
				Domains: []string{"domain", "domain:80"},
				Routes: []*route.Route{
					{
						Name: "foo",
						Action: &route.Route_Route{
							Route: &route.RouteAction{
								PrefixRewrite: "/foo",
							},
						},
					},
				},
			},
			{
				Name: "new-vhost",
			},
		},
		RequestHeadersToRemove: []string{"h1", "h2", "h3", "h4"},
	}
	sidecarInboundRC := &route.RouteConfiguration{
		Name: "inbound|http|80",
		VirtualHosts: []*route.VirtualHost{
			{
				Name: "vhost2",
			},
		},
	}
	patchedSidecarInboundRC := &route.RouteConfiguration{
		Name: "inbound|http|80",
		VirtualHosts: []*route.VirtualHost{
			{
				Name: "new-vhost",
			},
		},
	}

	gatewayRC := &route.RouteConfiguration{
		Name: "80",
		VirtualHosts: []*route.VirtualHost{
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
	patchedGatewayRC := &route.RouteConfiguration{
		Name: "80",
		VirtualHosts: []*route.VirtualHost{
			{
				Name:    "gateway",
				Domains: []string{"gateway", "domain:80"},
			},
			{
				Name: "new-vhost",
			},
		},
	}

	serviceDiscovery := memory.NewServiceDiscovery(nil)
	env := newTestEnvironment(serviceDiscovery, testMesh, buildEnvoyFilterConfigStore(configPatches))
	push := model.NewPushContext()
	push.InitContext(env, nil, nil)

	sidecarNode := &model.Proxy{Type: model.SidecarProxy, ConfigNamespace: "not-default"}
	gatewayNode := &model.Proxy{Type: model.Router, ConfigNamespace: "not-default"}

	type args struct {
		patchContext       networking.EnvoyFilter_PatchContext
		proxy              *model.Proxy
		push               *model.PushContext
		routeConfiguration *route.RouteConfiguration
	}
	tests := []struct {
		name string
		args args
		want *route.RouteConfiguration
	}{
		{
			name: "sidecar outbound rds patch",
			args: args{
				patchContext:       networking.EnvoyFilter_SIDECAR_OUTBOUND,
				proxy:              sidecarNode,
				push:               push,
				routeConfiguration: sidecarOutboundRC,
			},
			want: patchedSidecarOutputRC,
		},
		{
			name: "sidecar inbound rc patch",
			args: args{
				patchContext:       networking.EnvoyFilter_SIDECAR_INBOUND,
				proxy:              sidecarNode,
				push:               push,
				routeConfiguration: sidecarInboundRC,
			},
			want: patchedSidecarInboundRC,
		},
		{
			name: "gateway rds patch",
			args: args{
				patchContext:       networking.EnvoyFilter_GATEWAY,
				proxy:              gatewayNode,
				push:               push,
				routeConfiguration: gatewayRC,
			},
			want: patchedGatewayRC,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplyRouteConfigurationPatches(tt.args.patchContext, tt.args.proxy,
				tt.args.push, tt.args.routeConfiguration)
			if diff := cmp.Diff(tt.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("ApplyListenerPatches(): %s mismatch (-want +got):\n%s", tt.name, diff)
			}
		})
	}
}

func Test_routeMatch(t *testing.T) {
	type args struct {
		httpRoute *route.Route
		cp        *model.EnvoyFilterConfigPatchWrapper
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "route is nil",
			args: args{
				cp: &model.EnvoyFilterConfigPatchWrapper{
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
							RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
								Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
									Route: &networking.EnvoyFilter_RouteConfigurationMatch_RouteMatch{},
								},
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "full match by name",
			args: args{
				httpRoute: &route.Route{
					Name: "scooby",
				},
				cp: &model.EnvoyFilterConfigPatchWrapper{
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
							RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
								Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
									Route: &networking.EnvoyFilter_RouteConfigurationMatch_RouteMatch{
										Name: "scooby",
									},
								},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "full match by action",
			args: args{
				httpRoute: &route.Route{
					Action: &route.Route_Redirect{Redirect: &route.RedirectAction{}},
				},
				cp: &model.EnvoyFilterConfigPatchWrapper{
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
							RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
								Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
									Route: &networking.EnvoyFilter_RouteConfigurationMatch_RouteMatch{
										Action: networking.EnvoyFilter_RouteConfigurationMatch_RouteMatch_REDIRECT,
									},
								},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "mis match by action",
			args: args{
				httpRoute: &route.Route{
					Action: &route.Route_Redirect{Redirect: &route.RedirectAction{}},
				},
				cp: &model.EnvoyFilterConfigPatchWrapper{
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
							RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
								Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
									Route: &networking.EnvoyFilter_RouteConfigurationMatch_RouteMatch{
										Action: networking.EnvoyFilter_RouteConfigurationMatch_RouteMatch_ROUTE,
									},
								},
							},
						},
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := routeMatch(tt.args.httpRoute, tt.args.cp); got != tt.want {
				t.Errorf("routeMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}
