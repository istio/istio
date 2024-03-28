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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pkg/config/xds"
	"istio.io/istio/pkg/util/sets"
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
		portMap      model.GatewayPortMap
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
		{
			name: "http target port match",
			args: args{
				patchContext: networking.EnvoyFilter_GATEWAY,
				cp: &model.EnvoyFilterConfigPatchWrapper{
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
							RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
								PortNumber: 80,
							},
						},
					},
				},
				rc: &route.RouteConfiguration{Name: "http.8080"},
				portMap: map[int]sets.Set[int]{
					8080: {80: {}, 81: {}},
				},
			},
			want: true,
		},
		{
			name: "http target port no match",
			args: args{
				patchContext: networking.EnvoyFilter_GATEWAY,
				cp: &model.EnvoyFilterConfigPatchWrapper{
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
							RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
								PortNumber: 9090,
							},
						},
					},
				},
				rc: &route.RouteConfiguration{Name: "http.9090"},
				portMap: map[int]sets.Set[int]{
					8080: {80: {}, 81: {}},
				},
			},
			want: true,
		},
		{
			name: "https.443.app1.gw1.ns1",
			args: args{
				patchContext: networking.EnvoyFilter_GATEWAY,
				cp: &model.EnvoyFilterConfigPatchWrapper{
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
							RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
								PortNumber: 443,
							},
						},
					},
				},
				rc: &route.RouteConfiguration{Name: "http.8443"},
				portMap: map[int]sets.Set[int]{
					8443: {443: {}},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := routeConfigurationMatch(tt.args.patchContext, tt.args.rc, tt.args.cp, tt.args.portMap); got != tt.want {
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
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_OUTBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
					RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
						Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
							Name: "allow_any",
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
		{
			ApplyTo: networking.EnvoyFilter_HTTP_ROUTE,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_ANY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
					RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
						PortNumber: 9090,
						Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
							Name: "test.com",
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_ADD,
				Value:     buildPatchStruct(`{"name": "route4.0"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_HTTP_ROUTE,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_ANY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
					RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
						PortNumber: 9090,
						Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
							Name: "test.com",
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_INSERT_FIRST,
				Value:     buildPatchStruct(`{"name": "route0.0"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_HTTP_ROUTE,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_ANY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
					RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
						PortNumber: 9090,
						Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
							Name: "test.com",
							Route: &networking.EnvoyFilter_RouteConfigurationMatch_RouteMatch{
								Name: "route2.0",
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_INSERT_AFTER,
				Value:     buildPatchStruct(`{"name": "route2.5"}`),
			},
		},
		{
			ApplyTo: networking.EnvoyFilter_HTTP_ROUTE,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_ANY,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
					RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
						PortNumber: 9090,
						Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
							Name: "test.com",
							Route: &networking.EnvoyFilter_RouteConfigurationMatch_RouteMatch{
								Name: "route2.0",
							},
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_INSERT_BEFORE,
				Value:     buildPatchStruct(`{"name": "route1.5"}`),
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
	arrayInsert := &route.RouteConfiguration{
		Name: "9090",
		VirtualHosts: []*route.VirtualHost{
			{
				Name:    "test.com",
				Domains: []string{"domain"},
				Routes: []*route.Route{
					{
						Name: "route1.0",
						Action: &route.Route_Route{
							Route: &route.RouteAction{
								PrefixRewrite: "/",
							},
						},
					},
					{
						Name: "route2.0",
						Action: &route.Route_Redirect{
							Redirect: &route.RedirectAction{
								ResponseCode: 301,
							},
						},
					},
					{
						Name: "route3.0",
						Action: &route.Route_Redirect{
							Redirect: &route.RedirectAction{
								ResponseCode: 404,
							},
						},
					},
				},
			},
		},
	}
	patchedArrayInsert := &route.RouteConfiguration{
		Name: "9090",
		VirtualHosts: []*route.VirtualHost{
			{
				Name:    "test.com",
				Domains: []string{"domain", "domain:80"},
				Routes: []*route.Route{
					{
						Name: "route0.0",
					},
					{
						Name: "route1.0",
						Action: &route.Route_Route{
							Route: &route.RouteAction{
								PrefixRewrite: "/",
							},
						},
					},
					{
						Name: "route1.5",
					},
					{
						Name: "route2.0",
						Action: &route.Route_Redirect{
							Redirect: &route.RedirectAction{
								ResponseCode: 301,
							},
						},
					},
					{
						Name: "route2.5",
					},
					{
						Name: "route3.0",
						Action: &route.Route_Redirect{
							Redirect: &route.RedirectAction{
								ResponseCode: 404,
							},
						},
					},
					{
						Name: "route4.0",
					},
				},
			},
			{
				Name: "new-vhost",
			},
		},
	}

	serviceDiscovery := memory.NewServiceDiscovery()
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
		{
			name: "array insert patch",
			args: args{
				patchContext:       networking.EnvoyFilter_SIDECAR_OUTBOUND,
				proxy:              sidecarNode,
				push:               push,
				routeConfiguration: arrayInsert,
			},
			want: patchedArrayInsert,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			efw := tt.args.push.EnvoyFilters(tt.args.proxy)
			got := ApplyRouteConfigurationPatches(tt.args.patchContext, tt.args.proxy,
				efw, tt.args.routeConfiguration)
			if diff := cmp.Diff(tt.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("ApplyRouteConfigurationPatches(): %s mismatch (-want +got):\n%s", tt.name, diff)
			}
		})
	}
}

func TestReplaceVhost(t *testing.T) {
	configPatches := []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
		{
			ApplyTo: networking.EnvoyFilter_VIRTUAL_HOST,
			Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: networking.EnvoyFilter_SIDECAR_INBOUND,
				ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
					RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
						Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
							Name: "to-be-replaced",
						},
					},
				},
			},
			Patch: &networking.EnvoyFilter_Patch{
				Operation: networking.EnvoyFilter_Patch_REPLACE,
				Value: buildPatchStruct(`{
				"name":"replaced",
				"domains":["replaced.com"],
				"rate_limits": [
				  {
					"actions": [
					  {
						"request_headers": {
						  "header_name": ":path",
						  "descriptor_key": "PATH"
						}
					  }
					]
				  }
				]
			  }`),
			},
		},
	}

	sidecarInboundRCToBeReplaced := &route.RouteConfiguration{
		Name: "inbound|http|80",
		VirtualHosts: []*route.VirtualHost{
			{
				Name:    "to-be-replaced",
				Domains: []string{"xxx"},
				Routes: []*route.Route{
					{
						Name: "xxx",
						Action: &route.Route_Route{
							Route: &route.RouteAction{
								PrefixRewrite: "/xxx",
							},
						},
					},
				},
			},
		},
	}
	replacedSidecarInboundRC := &route.RouteConfiguration{
		Name: "inbound|http|80",
		VirtualHosts: []*route.VirtualHost{
			{
				Name:    "replaced",
				Domains: []string{"replaced.com"},
				RateLimits: []*route.RateLimit{
					{
						Actions: []*route.RateLimit_Action{
							{
								ActionSpecifier: &route.RateLimit_Action_RequestHeaders_{
									RequestHeaders: &route.RateLimit_Action_RequestHeaders{
										HeaderName:    ":path",
										DescriptorKey: "PATH",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	serviceDiscovery := memory.NewServiceDiscovery()
	env := newTestEnvironment(serviceDiscovery, testMesh, buildEnvoyFilterConfigStore(configPatches))
	push := model.NewPushContext()
	push.InitContext(env, nil, nil)

	sidecarNode := &model.Proxy{Type: model.SidecarProxy, ConfigNamespace: "not-default"}

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
			name: "sidecar inbound vhost replace",
			args: args{
				patchContext:       networking.EnvoyFilter_SIDECAR_INBOUND,
				proxy:              sidecarNode,
				push:               push,
				routeConfiguration: sidecarInboundRCToBeReplaced,
			},
			want: replacedSidecarInboundRC,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			efw := tt.args.push.EnvoyFilters(tt.args.proxy)
			got := ApplyRouteConfigurationPatches(tt.args.patchContext, tt.args.proxy,
				efw, tt.args.routeConfiguration)
			if diff := cmp.Diff(tt.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("ReplaceVhost(): %s mismatch (-want +got):\n%s", tt.name, diff)
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

func TestPatchHTTPRoute(t *testing.T) {
	sharedVHostRoutes := []*route.Route{
		{
			Name: "shared",
			Action: &route.Route_Route{
				Route: &route.RouteAction{
					PrefixRewrite: "/shared",
				},
			},
		},
	}
	type args struct {
		patchContext       networking.EnvoyFilter_PatchContext
		patches            map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper
		routeConfiguration *route.RouteConfiguration
		virtualHost        *route.VirtualHost
		routeIndex         int
		routesRemoved      *bool
		portMap            model.GatewayPortMap
		clonedVhostRoutes  bool
		sharedRoutesVHost  *route.VirtualHost
	}
	obj := &route.Route{}
	if err := xds.StructToMessage(buildPatchStruct(`{"route": { "prefix_rewrite": "/foo"}}`), obj, false); err != nil {
		t.Errorf("GogoStructToMessage error %v", err)
	}
	tests := []struct {
		name string
		args args
		want *route.VirtualHost
	}{
		{
			name: "normal",
			args: args{
				patchContext: networking.EnvoyFilter_GATEWAY,
				patches: map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper{
					networking.EnvoyFilter_HTTP_ROUTE: {
						{
							ApplyTo: networking.EnvoyFilter_HTTP_ROUTE,
							Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
								ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
									RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
										Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
											Name: "normal",
											Route: &networking.EnvoyFilter_RouteConfigurationMatch_RouteMatch{
												Action: networking.EnvoyFilter_RouteConfigurationMatch_RouteMatch_ROUTE,
											},
										},
									},
								},
							},
							Operation: networking.EnvoyFilter_Patch_MERGE,
							Value:     obj,
						},
					},
				},
				routeConfiguration: &route.RouteConfiguration{Name: "normal"},
				virtualHost: &route.VirtualHost{
					Name:    "normal",
					Domains: []string{"*"},
					Routes: []*route.Route{
						{
							Name: "normal",
							Action: &route.Route_Route{
								Route: &route.RouteAction{
									PrefixRewrite: "/normal",
								},
							},
						},
					},
				},
				routeIndex: 0,
				portMap: map[int]sets.Set[int]{
					8080: {},
				},
				clonedVhostRoutes: false,
			},
			want: &route.VirtualHost{
				Name:    "normal",
				Domains: []string{"*"},
				Routes: []*route.Route{
					{
						Name: "normal",
						Action: &route.Route_Route{
							Route: &route.RouteAction{
								PrefixRewrite: "/foo",
							},
						},
					},
				},
			},
		},
		{
			name: "shared",
			args: args{
				patchContext: networking.EnvoyFilter_GATEWAY,
				patches: map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper{
					networking.EnvoyFilter_HTTP_ROUTE: {
						{
							ApplyTo: networking.EnvoyFilter_HTTP_ROUTE,
							Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
								ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
									RouteConfiguration: &networking.EnvoyFilter_RouteConfigurationMatch{
										Vhost: &networking.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
											Name: "shared",
											Route: &networking.EnvoyFilter_RouteConfigurationMatch_RouteMatch{
												Action: networking.EnvoyFilter_RouteConfigurationMatch_RouteMatch_ROUTE,
											},
										},
									},
								},
							},
							Operation: networking.EnvoyFilter_Patch_MERGE,
							Value:     obj,
						},
					},
				},
				routeConfiguration: &route.RouteConfiguration{Name: "normal"},
				virtualHost: &route.VirtualHost{
					Name:    "shared",
					Domains: []string{"*"},
					Routes:  sharedVHostRoutes,
				},
				routeIndex: 0,
				portMap: map[int]sets.Set[int]{
					8080: {},
				},
				clonedVhostRoutes: false,
				sharedRoutesVHost: &route.VirtualHost{
					Name:    "foo",
					Domains: []string{"bar"},
					Routes:  sharedVHostRoutes,
				},
			},
			want: &route.VirtualHost{
				Name:    "shared",
				Domains: []string{"*"},
				Routes: []*route.Route{
					{
						Name: "shared",
						Action: &route.Route_Route{
							Route: &route.RouteAction{
								PrefixRewrite: "/foo",
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			savedSharedVHost := proto.Clone(tt.args.sharedRoutesVHost).(*route.VirtualHost)
			patchHTTPRoute(tt.args.patchContext, tt.args.patches, tt.args.routeConfiguration,
				tt.args.virtualHost, tt.args.routeIndex, tt.args.routesRemoved, tt.args.portMap, &tt.args.clonedVhostRoutes)
			if diff := cmp.Diff(tt.want, tt.args.virtualHost, protocmp.Transform()); diff != "" {
				t.Errorf("PatchHTTPRoute(): %s mismatch (-want +got):\n%s", tt.name, diff)
			}
			if diff := cmp.Diff(savedSharedVHost, tt.args.sharedRoutesVHost, protocmp.Transform()); diff != "" {
				t.Errorf("PatchHTTPRoute(): %s affect other virtualhosts (-want +got):\n%s", tt.name, diff)
			}
		})
	}
}

func TestCloneVhostRouteByRouteIndex(t *testing.T) {
	type args struct {
		vh1 *route.VirtualHost
		vh2 *route.VirtualHost
	}
	cloneRouter := route.Route{
		Name: "clone",
		Action: &route.Route_Route{
			Route: &route.RouteAction{
				PrefixRewrite: "/clone",
			},
		},
	}
	tests := []struct {
		name      string
		args      args
		wantClone bool
	}{
		{
			name: "clone",
			args: args{
				vh1: &route.VirtualHost{
					Name:    "vh1",
					Domains: []string{"*"},
					Routes: []*route.Route{
						&cloneRouter,
					},
				},
				vh2: &route.VirtualHost{
					Name:    "vh2",
					Domains: []string{"*"},
					Routes: []*route.Route{
						&cloneRouter,
					},
				},
			},
			wantClone: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cloneRouter.Name = "test"
			cloneVhostRouteByRouteIndex(tt.args.vh1, 0)
			if tt.args.vh1.Routes[0].Name != tt.args.vh2.Routes[0].Name && tt.wantClone {
				t.Errorf("CloneVhostRouteByRouteIndex(): %s (-wantClone +got):%v", tt.name, tt.wantClone)
			}
		})
	}
}
