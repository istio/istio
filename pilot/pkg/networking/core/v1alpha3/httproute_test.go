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

package v1alpha3

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"

	meshapi "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/visibility"
)

func TestGenerateVirtualHostDomains(t *testing.T) {
	cases := []struct {
		name    string
		service *model.Service
		port    int
		node    *model.Proxy
		want    []string
	}{
		{
			name: "same domain",
			service: &model.Service{
				Hostname:     "foo.local.campus.net",
				MeshExternal: false,
			},
			port: 80,
			node: &model.Proxy{
				DNSDomain: "local.campus.net",
			},
			want: []string{
				"foo.local.campus.net", "foo.local.campus.net:80",
				"foo", "foo:80",
			},
		},
		{
			name: "different domains with some shared dns",
			service: &model.Service{
				Hostname:     "foo.local.campus.net",
				MeshExternal: false,
			},
			port: 80,
			node: &model.Proxy{
				DNSDomain: "remote.campus.net",
			},
			want: []string{
				"foo.local.campus.net",
				"foo.local.campus.net:80",
				"foo.local",
				"foo.local:80",
				"foo.local.campus",
				"foo.local.campus:80",
			},
		},
		{
			name: "different domains with no shared dns",
			service: &model.Service{
				Hostname:     "foo.local.campus.net",
				MeshExternal: false,
			},
			port: 80,
			node: &model.Proxy{
				DNSDomain: "example.com",
			},
			want: []string{"foo.local.campus.net", "foo.local.campus.net:80"},
		},
		{
			name: "k8s service with default domain",
			service: &model.Service{
				Hostname:     "echo.default.svc.cluster.local",
				MeshExternal: false,
			},
			port: 8123,
			node: &model.Proxy{
				DNSDomain: "default.svc.cluster.local",
			},
			want: []string{
				"echo.default.svc.cluster.local",
				"echo.default.svc.cluster.local:8123",
				"echo",
				"echo:8123",
				"echo.default.svc",
				"echo.default.svc:8123",
				"echo.default",
				"echo.default:8123",
			},
		},
		{
			name: "non-k8s service",
			service: &model.Service{
				Hostname:     "foo.default.svc.bar.baz",
				MeshExternal: false,
			},
			port: 8123,
			node: &model.Proxy{
				DNSDomain: "default.svc.cluster.local",
			},
			want: []string{
				"foo.default.svc.bar.baz",
				"foo.default.svc.bar.baz:8123",
			},
		},
		{
			name: "k8s service with default domain and different namespace",
			service: &model.Service{
				Hostname:     "echo.default.svc.cluster.local",
				MeshExternal: false,
			},
			port: 8123,
			node: &model.Proxy{
				DNSDomain: "mesh.svc.cluster.local",
			},
			want: []string{
				"echo.default.svc.cluster.local",
				"echo.default.svc.cluster.local:8123",
				"echo.default",
				"echo.default:8123",
				"echo.default.svc",
				"echo.default.svc:8123",
			},
		},
		{
			name: "k8s service with custom domain 2",
			service: &model.Service{
				Hostname:     "google.local",
				MeshExternal: false,
			},
			port: 8123,
			node: &model.Proxy{
				DNSDomain: "foo.svc.custom.k8s.local",
			},
			want: []string{"google.local", "google.local:8123"},
		},
		{
			name: "ipv4 domain",
			service: &model.Service{
				Hostname:     "1.2.3.4",
				MeshExternal: false,
			},
			port: 8123,
			node: &model.Proxy{
				DNSDomain: "example.com",
			},
			want: []string{"1.2.3.4", "1.2.3.4:8123"},
		},
		{
			name: "ipv6 domain",
			service: &model.Service{
				Hostname:     "2406:3003:2064:35b8:864:a648:4b96:e37d",
				MeshExternal: false,
			},
			port: 8123,
			node: &model.Proxy{
				DNSDomain: "example.com",
			},
			want: []string{"[2406:3003:2064:35b8:864:a648:4b96:e37d]", "[2406:3003:2064:35b8:864:a648:4b96:e37d]:8123"},
		},
		{
			name: "back subset of cluster domain in address",
			service: &model.Service{
				Hostname:     "aaa.example.local",
				MeshExternal: true,
			},
			port: 7777,
			node: &model.Proxy{
				DNSDomain: "tests.svc.cluster.local",
			},
			want: []string{"aaa.example.local", "aaa.example.local:7777"},
		},
		{
			name: "front subset of cluster domain in address",
			service: &model.Service{
				Hostname:     "aaa.example.my",
				MeshExternal: true,
			},
			port: 7777,
			node: &model.Proxy{
				DNSDomain: "tests.svc.my.long.domain.suffix",
			},
			want: []string{"aaa.example.my", "aaa.example.my:7777"},
		},
		{
			name: "large subset of cluster domain in address",
			service: &model.Service{
				Hostname:     "aaa.example.my.long",
				MeshExternal: true,
			},
			port: 7777,
			node: &model.Proxy{
				DNSDomain: "tests.svc.my.long.domain.suffix",
			},
			want: []string{"aaa.example.my.long", "aaa.example.my.long:7777"},
		},
		{
			name: "no overlap of cluster domain in address",
			service: &model.Service{
				Hostname:     "aaa.example.com",
				MeshExternal: true,
			},
			port: 7777,
			node: &model.Proxy{
				DNSDomain: "tests.svc.cluster.local",
			},
			want: []string{"aaa.example.com", "aaa.example.com:7777"},
		},
	}

	testFn := func(service *model.Service, port int, node *model.Proxy, want []string) error {
		out, _ := generateVirtualHostDomains(service, port, node)
		if !reflect.DeepEqual(out, want) {
			return fmt.Errorf("unexpected virtual hosts:\ngot  %v\nwant %v", out, want)
		}
		return nil
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if err := testFn(c.service, c.port, c.node, c.want); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestSidecarOutboundHTTPRouteConfigWithDuplicateHosts(t *testing.T) {
	virtualServiceSpec := &networking.VirtualService{
		Hosts:    []string{"test-duplicate-domains.default.svc.cluster.local", "test-duplicate-domains.default"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "test-duplicate-domains.default",
						},
					},
				},
			},
		},
	}
	virtualServiceSpecDefault := &networking.VirtualService{
		Hosts:    []string{"test.default"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "test.default",
						},
					},
				},
			},
		},
	}

	cases := []struct {
		name                string
		services            []*model.Service
		config              []config.Config
		expectedHosts       map[string][]string
		expectedDestination map[string]string
	}{
		{
			"more exact first",
			[]*model.Service{
				buildHTTPService("test.local", visibility.Public, "", "default", 80),
				buildHTTPService("test", visibility.Public, "", "default", 80),
			},
			nil,
			map[string][]string{
				"allow_any": {"*"},
				// BUG: test should be below
				"test.local:80": {"test.local", "test.local:80"},
				"test:80":       {"test", "test:80"},
			},
			map[string]string{
				"allow_any":     "PassthroughCluster",
				"test.local:80": "outbound|80||test.local",
				"test:80":       "outbound|80||test",
			},
		},
		{
			"more exact first with full cluster domain",
			[]*model.Service{
				buildHTTPService("test.default.svc.cluster.local", visibility.Public, "", "default", 80),
				buildHTTPService("test", visibility.Public, "", "default", 80),
			},
			nil,
			map[string][]string{
				"allow_any": {"*"},
				"test.default.svc.cluster.local:80": {
					"test.default.svc.cluster.local", "test.default.svc.cluster.local:80",
					"test.default.svc", "test.default.svc:80",
					"test.default", "test.default:80",
				},
				"test:80": {"test", "test:80"},
			},
			map[string]string{
				"allow_any":                         "PassthroughCluster",
				"test.default.svc.cluster.local:80": "outbound|80||test.default.svc.cluster.local",
				"test:80":                           "outbound|80||test",
			},
		},
		{
			"more exact second",
			[]*model.Service{
				buildHTTPService("test", visibility.Public, "", "default", 80),
				buildHTTPService("test.local", visibility.Public, "", "default", 80),
			},
			nil,
			map[string][]string{
				"allow_any":     {"*"},
				"test.local:80": {"test.local", "test.local:80"},
				"test:80":       {"test", "test:80"},
			},
			map[string]string{
				"allow_any":     "PassthroughCluster",
				"test.local:80": "outbound|80||test.local",
				"test:80":       "outbound|80||test",
			},
		},
		{
			"virtual service",
			[]*model.Service{
				buildHTTPService("test-duplicate-domains.default.svc.cluster.local", visibility.Public, "", "default", 80),
			},
			[]config.Config{{
				Meta: config.Meta{
					GroupVersionKind: gvk.VirtualService,
					Name:             "acme",
				},
				Spec: virtualServiceSpec,
			}},
			map[string][]string{
				"allow_any": {"*"},
				"test-duplicate-domains.default.svc.cluster.local:80": {
					"test-duplicate-domains.default.svc.cluster.local", "test-duplicate-domains.default.svc.cluster.local:80",
					"test-duplicate-domains", "test-duplicate-domains:80",
					"test-duplicate-domains.default.svc", "test-duplicate-domains.default.svc:80",
				},
				"test-duplicate-domains.default:80": {"test-duplicate-domains.default", "test-duplicate-domains.default:80"},
			},
			map[string]string{
				"allow_any": "PassthroughCluster",
				// Virtual service destination takes precedence
				"test-duplicate-domains.default.svc.cluster.local:80": "outbound|80||test-duplicate-domains.default",
				"test-duplicate-domains.default:80":                   "outbound|80||test-duplicate-domains.default",
			},
		},
		{
			"virtual service conflict",
			[]*model.Service{
				buildHTTPService("test.default.svc.cluster.local", visibility.Public, "", "default", 80),
			},
			[]config.Config{{
				Meta: config.Meta{
					GroupVersionKind: gvk.VirtualService,
					Name:             "acme",
				},
				Spec: virtualServiceSpecDefault,
			}},
			map[string][]string{
				"allow_any": {"*"},
				"test.default.svc.cluster.local:80": {
					"test.default.svc.cluster.local", "test.default.svc.cluster.local:80",
					"test", "test:80",
					"test.default.svc", "test.default.svc:80",
				},
				"test.default:80": {"test.default", "test.default:80"},
			},
			map[string]string{
				"allow_any": "PassthroughCluster",
				// From the service, go to the service
				"test.default.svc.cluster.local:80": "outbound|80||test.default.svc.cluster.local",
				"test.default:80":                   "outbound|80||test.default",
			},
		},
		{
			"multiple ports",
			[]*model.Service{
				buildHTTPService("test.local", visibility.Public, "", "default", 70, 80, 90),
			},
			nil,
			map[string][]string{
				"allow_any":     {"*"},
				"test.local:80": {"test.local", "test.local:80"},
			},
			map[string]string{
				"allow_any":     "PassthroughCluster",
				"test.local:80": "outbound|80||test.local",
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// ensure services are ordered
			t0 := time.Now()
			for _, svc := range tt.services {
				svc.CreationTime = t0
				t0 = t0.Add(time.Minute)
			}
			cg := NewConfigGenTest(t, TestOptions{
				Services: tt.services,
				Configs:  tt.config,
			})

			vHostCache := make(map[int][]*route.VirtualHost)
			routeName := "80"
			resource, _ := cg.ConfigGen.buildSidecarOutboundHTTPRouteConfig(
				cg.SetupProxy(nil), &model.PushRequest{Push: cg.PushContext()}, "80", vHostCache, nil, nil)
			routeCfg := &route.RouteConfiguration{}
			resource.Resource.UnmarshalTo(routeCfg)
			xdstest.ValidateRouteConfiguration(t, routeCfg)
			if routeCfg == nil {
				t.Fatalf("got nil route for %s", routeName)
			}

			got := map[string][]string{}
			clusters := map[string]string{}
			for _, vh := range routeCfg.VirtualHosts {
				got[vh.Name] = vh.Domains
				clusters[vh.Name] = vh.GetRoutes()[0].GetRoute().GetCluster()
			}

			if !reflect.DeepEqual(tt.expectedHosts, got) {
				t.Fatalf("unexpected virtual hosts\n%v, wanted\n%v", got, tt.expectedHosts)
			}

			if !reflect.DeepEqual(tt.expectedDestination, clusters) {
				t.Fatalf("unexpected destinations\n%v, wanted\n%v", clusters, tt.expectedDestination)
			}
		})
	}
}

func TestSidecarOutboundHTTPRouteConfig(t *testing.T) {
	services := []*model.Service{
		buildHTTPService("bookinfo.com", visibility.Public, wildcardIP, "default", 9999, 70),
		buildHTTPService("private.com", visibility.Private, wildcardIP, "default", 9999, 80),
		buildHTTPService("test.com", visibility.Public, "8.8.8.8", "not-default", 8080),
		buildHTTPService("test-private.com", visibility.Private, "9.9.9.9", "not-default", 80, 70),
		buildHTTPService("test-private-2.com", visibility.Private, "9.9.9.10", "not-default", 60),
		buildHTTPService("test-headless.com", visibility.Public, wildcardIP, "not-default", 8888),
		buildHTTPService("service-A.default.svc.cluster.local", visibility.Public, "", "default", 7777),
	}

	sidecarConfig := &config.Config{
		Meta: config.Meta{
			Name:             "foo",
			Namespace:        "not-default",
			GroupVersionKind: gvk.Sidecar,
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						// A port that is not in any of the services
						Number:   9000,
						Protocol: "HTTP",
						Name:     "something",
					},
					Bind:  "1.1.1.1",
					Hosts: []string{"*/bookinfo.com"},
				},
				{
					Port: &networking.Port{
						// Unix domain socket listener
						Number:   0,
						Protocol: "HTTP",
						Name:     "something",
					},
					Bind:  "unix://foo/bar/baz",
					Hosts: []string{"*/bookinfo.com"},
				},
				{
					Port: &networking.Port{
						// Unix domain socket listener
						Number:   0,
						Protocol: "HTTP",
						Name:     "something",
					},
					Bind:  "unix://foo/bar/headless",
					Hosts: []string{"*/test-headless.com"},
				},
				{
					Port: &networking.Port{
						// A port that is in one of the services
						Number:   8080,
						Protocol: "HTTP",
						Name:     "foo",
					},
					Hosts: []string{"default/bookinfo.com", "not-default/test.com"},
				},
				{
					// Wildcard egress importing from all namespaces
					Hosts: []string{"*/*"},
				},
			},
		},
	}
	sidecarConfigWithWildcard := &config.Config{
		Meta: config.Meta{
			Name:             "foo",
			Namespace:        "not-default",
			GroupVersionKind: gvk.Sidecar,
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   7443,
						Protocol: "HTTP",
						Name:     "something",
					},
					Hosts: []string{"*/*"},
				},
			},
		},
	}
	sidecarConfigWitHTTPProxy := &config.Config{
		Meta: config.Meta{
			Name:             "foo",
			Namespace:        "not-default",
			GroupVersionKind: gvk.Sidecar,
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Number:   7443,
						Protocol: "HTTP_PROXY",
						Name:     "something",
					},
					Hosts: []string{"*/*"},
				},
			},
		},
	}
	sidecarConfigWithRegistryOnly := &config.Config{
		Meta: config.Meta{
			Name:             "foo",
			Namespace:        "not-default",
			GroupVersionKind: gvk.Sidecar,
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						// A port that is not in any of the services
						Number:   9000,
						Protocol: "HTTP",
						Name:     "something",
					},
					Bind:  "1.1.1.1",
					Hosts: []string{"*/bookinfo.com"},
				},
				{
					Port: &networking.Port{
						// Unix domain socket listener
						Number:   0,
						Protocol: "HTTP",
						Name:     "something",
					},
					Bind:  "unix://foo/bar/baz",
					Hosts: []string{"*/bookinfo.com"},
				},
				{
					Port: &networking.Port{
						Number:   0,
						Protocol: "HTTP",
						Name:     "something",
					},
					Bind:  "unix://foo/bar/headless",
					Hosts: []string{"*/test-headless.com"},
				},
				{
					Port: &networking.Port{
						Number:   18888,
						Protocol: "HTTP",
						Name:     "foo",
					},
					Hosts: []string{"*/test-headless.com"},
				},
				{
					// Wildcard egress importing from all namespaces
					Hosts: []string{"*/*"},
				},
			},
			OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_REGISTRY_ONLY,
			},
		},
	}
	sidecarConfigWithAllowAny := &config.Config{
		Meta: config.Meta{
			Name:             "foo",
			Namespace:        "not-default",
			GroupVersionKind: gvk.Sidecar,
		},
		Spec: &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						// A port that is not in any of the services
						Number:   9000,
						Protocol: "HTTP",
						Name:     "something",
					},
					Bind:  "1.1.1.1",
					Hosts: []string{"*/bookinfo.com"},
				},
				{
					Port: &networking.Port{
						// Unix domain socket listener
						Number:   0,
						Protocol: "HTTP",
						Name:     "something",
					},
					Bind:  "unix://foo/bar/baz",
					Hosts: []string{"*/bookinfo.com"},
				},
				{
					Port: &networking.Port{
						// A port that is in one of the services
						Number:   8080,
						Protocol: "HTTP",
						Name:     "foo",
					},
					Hosts: []string{"default/bookinfo.com", "not-default/test.com"},
				},
				{
					// Wildcard egress importing from all namespaces
					Hosts: []string{"*/*"},
				},
			},
			OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
			},
		},
	}
	virtualServiceSpec1 := &networking.VirtualService{
		Hosts:    []string{"test-private-2.com"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							// Subset: "some-subset",
							Host: "example.org",
							Port: &networking.PortSelector{
								Number: 61,
							},
						},
						Weight: 100,
					},
				},
			},
		},
	}
	virtualServiceSpec2 := &networking.VirtualService{
		Hosts:    []string{"test-private-2.com"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "test.org",
							Port: &networking.PortSelector{
								Number: 62,
							},
						},
						Weight: 100,
					},
				},
			},
		},
	}
	virtualServiceSpec3 := &networking.VirtualService{
		Hosts:    []string{"test-private-3.com"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "test.org",
							Port: &networking.PortSelector{
								Number: 63,
							},
						},
						Weight: 100,
					},
				},
			},
		},
	}
	virtualServiceSpec4 := &networking.VirtualService{
		Hosts:    []string{"test-headless.com", "example.com"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "test.org",
							Port: &networking.PortSelector{
								Number: 64,
							},
						},
						Weight: 100,
					},
				},
			},
		},
	}
	virtualServiceSpec5 := &networking.VirtualService{
		Hosts:    []string{"test-svc.testns.svc.cluster.local"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "test-svc.testn.svc.cluster.local",
						},
						Weight: 100,
					},
				},
			},
		},
	}
	virtualServiceSpec6 := &networking.VirtualService{
		Hosts:    []string{"match-no-service"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "non-exist-service",
						},
						Weight: 100,
					},
				},
			},
		},
	}
	virtualServiceSpec7 := &networking.VirtualService{
		Hosts:    []string{"service-A.default.svc.cluster.local", "service-A.v2", "service-A.v3"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Match: []*networking.HTTPMatchRequest{
					{
						Headers: map[string]*networking.StringMatch{":authority": {MatchType: &networking.StringMatch_Exact{Exact: "service-A.v2"}}},
					},
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host:   "service-A",
							Subset: "v2",
						},
					},
				},
			},
			{
				Match: []*networking.HTTPMatchRequest{
					{
						Headers: map[string]*networking.StringMatch{":authority": {MatchType: &networking.StringMatch_Exact{Exact: "service-A.v3"}}},
					},
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host:   "service-A",
							Subset: "v3",
						},
					},
				},
			},
		},
	}
	virtualService1 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme2-v1",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec1,
	}
	virtualService2 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme-v2",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec2,
	}
	virtualService3 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme-v3",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec3,
	}
	virtualService4 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme-v4",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec4,
	}
	virtualService5 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme-v3",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec5,
	}
	virtualService6 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme-v3",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec6,
	}
	virtualService7 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.VirtualService,
			Name:             "vs-1",
			Namespace:        "default",
		},
		Spec: virtualServiceSpec7,
	}

	// With the config above, RDS should return a valid route for the following route names
	// port 9000 - [bookinfo.com:9999, *.bookinfo.com:9990], [bookinfo.com:70, *.bookinfo.com:70] but no bookinfo.com
	// unix://foo/bar/baz - [bookinfo.com:9999, *.bookinfo.com:9999], [bookinfo.com:70, *.bookinfo.com:70] but no bookinfo.com
	// port 8080 - [test.com, test.com:8080, 8.8.8.8, 8.8.8.8:8080], but no bookinfo.com or test.com
	// port 9999 - [bookinfo.com, bookinfo.com:9999, *.bookinfo.com, *.bookinfo.com:9999]
	// port 80 - [test-private.com, test-private.com:80, 9.9.9.9:80, 9.9.9.9]
	// port 70 - [test-private.com, test-private.com:70, 9.9.9.9, 9.9.9.9:70], [bookinfo.com, bookinfo.com:70]

	// Without sidecar config [same as wildcard egress listener], expect routes
	// 9999 - [bookinfo.com, bookinfo.com:9999, *.bookinfo.com, *.bookinfo.com:9999],
	// 8080 - [test.com, test.com:8080, 8.8.8.8, 8.8.8.8:8080]
	// 80 - [test-private.com, test-private.com:80, 9.9.9.9:80, 9.9.9.9]
	// 70 - [bookinfo.com, bookinfo.com:70, *.bookinfo.com:70],[test-private.com, test-private.com:70, 9.9.9.9:70, 9.9.9.9]
	cases := []struct {
		name                  string
		routeName             string
		sidecarConfig         *config.Config
		virtualServiceConfigs []*config.Config
		// virtualHost Name and domains
		expectedHosts  map[string]map[string]bool
		expectedRoutes int
		registryOnly   bool
	}{
		{
			name:                  "sidecar config port that is not in any service",
			routeName:             "9000",
			sidecarConfig:         sidecarConfig,
			virtualServiceConfigs: nil,
			expectedHosts: map[string]map[string]bool{
				"block_all": {
					"*": true,
				},
			},
			registryOnly: true,
		},
		{
			name:                  "sidecar config with unix domain socket listener",
			routeName:             "unix://foo/bar/baz",
			sidecarConfig:         sidecarConfig,
			virtualServiceConfigs: nil,
			expectedHosts: map[string]map[string]bool{
				"bookinfo.com:9999": {"bookinfo.com:9999": true, "*.bookinfo.com:9999": true},
				"bookinfo.com:70":   {"bookinfo.com:70": true, "*.bookinfo.com:70": true},
				"allow_any": {
					"*": true,
				},
			},
		},
		{
			name:                  "sidecar config port that is in one of the services",
			routeName:             "8080",
			sidecarConfig:         sidecarConfig,
			virtualServiceConfigs: nil,
			expectedHosts: map[string]map[string]bool{
				"test.com:8080": {"test.com": true, "test.com:8080": true, "8.8.8.8": true, "8.8.8.8:8080": true},
				"block_all": {
					"*": true,
				},
			},
			registryOnly: true,
		},
		{
			name:                  "sidecar config with fallthrough and registry only and allow any mesh config",
			routeName:             "80",
			sidecarConfig:         sidecarConfigWithRegistryOnly,
			virtualServiceConfigs: nil,
			expectedHosts: map[string]map[string]bool{
				"test-private.com:80": {
					"test-private.com": true, "test-private.com:80": true, "9.9.9.9": true, "9.9.9.9:80": true,
				},
				"block_all": {
					"*": true,
				},
			},
			registryOnly: false,
		},
		{
			name:                  "sidecar config with fallthrough and allow any and registry only mesh config",
			routeName:             "80",
			sidecarConfig:         sidecarConfigWithAllowAny,
			virtualServiceConfigs: nil,
			expectedHosts: map[string]map[string]bool{
				"test-private.com:80": {
					"test-private.com": true, "test-private.com:80": true, "9.9.9.9": true, "9.9.9.9:80": true,
				},
				"allow_any": {
					"*": true,
				},
			},
			registryOnly: false,
		},

		{
			name:                  "sidecar config with allow any and virtual service includes non existing service",
			routeName:             "80",
			sidecarConfig:         sidecarConfigWithAllowAny,
			virtualServiceConfigs: []*config.Config{&virtualService6},
			expectedHosts: map[string]map[string]bool{
				// does not include `match-no-service`
				"test-private.com:80": {
					"test-private.com": true, "test-private.com:80": true, "9.9.9.9": true, "9.9.9.9:80": true,
				},
				"match-no-service.not-default:80": {"match-no-service.not-default": true, "match-no-service.not-default:80": true},
				"allow_any": {
					"*": true,
				},
			},
			registryOnly: false,
		},

		{
			name:                  "sidecar config with allow any and virtual service includes non existing service",
			routeName:             "80",
			sidecarConfig:         sidecarConfigWithAllowAny,
			virtualServiceConfigs: []*config.Config{&virtualService6},
			expectedHosts: map[string]map[string]bool{
				// does not include `match-no-service`
				"test-private.com:80": {
					"test-private.com": true, "test-private.com:80": true, "9.9.9.9": true, "9.9.9.9:80": true,
				},
				"match-no-service.not-default:80": {"match-no-service.not-default": true, "match-no-service.not-default:80": true},
				"allow_any": {
					"*": true,
				},
			},
			registryOnly: false,
		},

		{
			name:                  "wildcard egress importing from all namespaces: 9999",
			routeName:             "9999",
			sidecarConfig:         sidecarConfig,
			virtualServiceConfigs: nil,
			expectedHosts: map[string]map[string]bool{
				"bookinfo.com:9999": {
					"bookinfo.com:9999": true, "bookinfo.com": true,
					"*.bookinfo.com:9999": true, "*.bookinfo.com": true,
				},
				"block_all": {
					"*": true,
				},
			},
			registryOnly: true,
		},
		{
			name:                  "wildcard egress importing from all namespaces: 80",
			routeName:             "80",
			sidecarConfig:         sidecarConfig,
			virtualServiceConfigs: nil,
			expectedHosts: map[string]map[string]bool{
				"test-private.com:80": {
					"test-private.com": true, "test-private.com:80": true, "9.9.9.9": true, "9.9.9.9:80": true,
				},
				"block_all": {
					"*": true,
				},
			},
			registryOnly: true,
		},
		{
			name:                  "wildcard egress importing from all namespaces: 70",
			routeName:             "70",
			sidecarConfig:         sidecarConfig,
			virtualServiceConfigs: nil,
			expectedHosts: map[string]map[string]bool{
				"test-private.com:70": {
					"test-private.com": true, "test-private.com:70": true, "9.9.9.9": true, "9.9.9.9:70": true,
				},
				"bookinfo.com:70": {
					"bookinfo.com": true, "bookinfo.com:70": true,
					"*.bookinfo.com": true, "*.bookinfo.com:70": true,
				},
				"block_all": {
					"*": true,
				},
			},
			registryOnly: true,
		},
		{
			name:                  "no sidecar config - import public service from other namespaces: 9999",
			routeName:             "9999",
			sidecarConfig:         nil,
			virtualServiceConfigs: nil,
			expectedHosts: map[string]map[string]bool{
				"bookinfo.com:9999": {
					"bookinfo.com:9999": true, "bookinfo.com": true,
					"*.bookinfo.com:9999": true, "*.bookinfo.com": true,
				},
				"block_all": {
					"*": true,
				},
			},
			registryOnly: true,
		},
		{
			name:                  "no sidecar config - import public service from other namespaces: 8080",
			routeName:             "8080",
			sidecarConfig:         nil,
			virtualServiceConfigs: nil,
			expectedHosts: map[string]map[string]bool{
				"test.com:8080": {
					"test.com:8080": true, "test.com": true, "8.8.8.8": true, "8.8.8.8:8080": true,
				},
				"block_all": {
					"*": true,
				},
			},
			registryOnly: true,
		},
		{
			name:                  "no sidecar config - import public services from other namespaces: 80",
			routeName:             "80",
			sidecarConfig:         nil,
			virtualServiceConfigs: nil,
			expectedHosts: map[string]map[string]bool{
				"test-private.com:80": {
					"test-private.com": true, "test-private.com:80": true, "9.9.9.9": true, "9.9.9.9:80": true,
				},
				"block_all": {
					"*": true,
				},
			},
			registryOnly: true,
		},
		{
			name:                  "no sidecar config - import public services from other namespaces: 70",
			routeName:             "70",
			sidecarConfig:         nil,
			virtualServiceConfigs: nil,
			expectedHosts: map[string]map[string]bool{
				"test-private.com:70": {
					"test-private.com": true, "test-private.com:70": true, "9.9.9.9": true, "9.9.9.9:70": true,
				},
				"bookinfo.com:70": {
					"bookinfo.com": true, "bookinfo.com:70": true,
					"*.bookinfo.com": true, "*.bookinfo.com:70": true,
				},
				"block_all": {
					"*": true,
				},
			},
			registryOnly: true,
		},
		{
			name:                  "no sidecar config - import public services from other namespaces: 70 with sniffing",
			routeName:             "test-private.com:70",
			sidecarConfig:         nil,
			virtualServiceConfigs: nil,
			expectedHosts: map[string]map[string]bool{
				"test-private.com:70": {
					"*": true,
				},
			},
			registryOnly: true,
		},
		{
			name:                  "no sidecar config - import public services from other namespaces: 80 with fallthrough",
			routeName:             "80",
			sidecarConfig:         nil,
			virtualServiceConfigs: nil,
			expectedHosts: map[string]map[string]bool{
				"test-private.com:80": {
					"test-private.com": true, "test-private.com:80": true, "9.9.9.9": true, "9.9.9.9:80": true,
				},
				"allow_any": {
					"*": true,
				},
			},
			registryOnly: false,
		},
		{
			name:                  "no sidecar config - import public services from other namespaces: 80 with fallthrough and registry only",
			routeName:             "80",
			sidecarConfig:         nil,
			virtualServiceConfigs: nil,
			expectedHosts: map[string]map[string]bool{
				"test-private.com:80": {
					"test-private.com": true, "test-private.com:80": true, "9.9.9.9": true, "9.9.9.9:80": true,
				},
				"block_all": {
					"*": true,
				},
			},
			registryOnly: true,
		},
		{
			name:                  "no sidecar config with virtual services with duplicate entries",
			routeName:             "60",
			sidecarConfig:         nil,
			virtualServiceConfigs: []*config.Config{&virtualService1, &virtualService2},
			expectedHosts: map[string]map[string]bool{
				"test-private-2.com:60": {
					"test-private-2.com": true, "test-private-2.com:60": true, "9.9.9.10": true, "9.9.9.10:60": true,
				},
				"block_all": {
					"*": true,
				},
			},
			registryOnly: true,
		},
		{
			name:                  "no sidecar config with virtual services with no service in registry",
			routeName:             "80", // no service for the host in registry; use port 80 by default
			sidecarConfig:         nil,
			virtualServiceConfigs: []*config.Config{&virtualService3},
			expectedHosts: map[string]map[string]bool{
				"test-private.com:80": {
					"test-private.com": true, "test-private.com:80": true, "9.9.9.9": true, "9.9.9.9:80": true,
				},
				"test-private-3.com:80": {
					"test-private-3.com": true, "test-private-3.com:80": true,
				},
				"block_all": {
					"*": true,
				},
			},
			registryOnly: true,
		},
		{
			name:                  "no sidecar config - import headless service from other namespaces: 8888",
			routeName:             "8888",
			sidecarConfig:         nil,
			virtualServiceConfigs: nil,
			expectedHosts: map[string]map[string]bool{
				"test-headless.com:8888": {
					"test-headless.com": true, "test-headless.com:8888": true, "*.test-headless.com": true, "*.test-headless.com:8888": true,
				},
				"block_all": {
					"*": true,
				},
			},
			registryOnly: true,
		},
		{
			name:                  "no sidecar config with virtual services - import headless service from other namespaces: 8888",
			routeName:             "8888",
			sidecarConfig:         nil,
			virtualServiceConfigs: []*config.Config{&virtualService4},
			expectedHosts: map[string]map[string]bool{
				"test-headless.com:8888": {
					"test-headless.com": true, "test-headless.com:8888": true, "*.test-headless.com": true, "*.test-headless.com:8888": true,
				},
				"example.com:8888": {
					"example.com": true, "example.com:8888": true,
				},
				"block_all": {
					"*": true,
				},
			},
			registryOnly: true,
		},
		{
			name:                  "sidecar config with unix domain socket listener - import headless service",
			routeName:             "unix://foo/bar/headless",
			sidecarConfig:         sidecarConfig,
			virtualServiceConfigs: nil,
			expectedHosts: map[string]map[string]bool{
				"test-headless.com:8888": {"test-headless.com:8888": true, "*.test-headless.com:8888": true},
				"block_all": {
					"*": true,
				},
			},
			registryOnly: true,
		},
		{
			name:                  "sidecar config port - import headless service",
			routeName:             "18888",
			sidecarConfig:         sidecarConfigWithRegistryOnly,
			virtualServiceConfigs: nil,
			expectedHosts: map[string]map[string]bool{
				"block_all": {
					"*": true,
				},
			},
			registryOnly: true,
		},
		{
			name:                  "wild card sidecar config, with non matching virtual service",
			routeName:             "7443",
			sidecarConfig:         sidecarConfigWithWildcard,
			virtualServiceConfigs: []*config.Config{&virtualService5},
			expectedHosts: map[string]map[string]bool{
				"block_all": {
					"*": true,
				},
			},
			registryOnly: true,
		},
		{
			name:                  "http proxy sidecar config, with non matching virtual service",
			routeName:             "7443",
			sidecarConfig:         sidecarConfigWitHTTPProxy,
			virtualServiceConfigs: []*config.Config{&virtualService5},
			expectedHosts: map[string]map[string]bool{
				"bookinfo.com:9999":      {"bookinfo.com:9999": true, "*.bookinfo.com:9999": true},
				"bookinfo.com:70":        {"bookinfo.com:70": true, "*.bookinfo.com:70": true},
				"test-headless.com:8888": {"test-headless.com:8888": true, "*.test-headless.com:8888": true},
				"test-private-2.com:60": {
					"test-private-2.com:60": true, "9.9.9.10:60": true,
				},
				"test-private.com:70": {
					"test-private.com:70": true, "9.9.9.9:70": true,
				},
				"test-private.com:80": {
					"test-private.com": true, "test-private.com:80": true, "9.9.9.9": true, "9.9.9.9:80": true,
				},
				"test.com:8080": {
					"test.com:8080": true, "8.8.8.8:8080": true,
				},
				"service-A.default.svc.cluster.local:7777": {
					"service-A.default.svc.cluster.local:7777": true,
				},
				"block_all": {
					"*": true,
				},
			},
			registryOnly: true,
		},
		{
			name:                  "virtual service hosts with subsets and with existing service",
			routeName:             "7777",
			sidecarConfig:         sidecarConfigWithAllowAny,
			virtualServiceConfigs: []*config.Config{&virtualService7},
			expectedHosts: map[string]map[string]bool{
				"allow_any": {
					"*": true,
				},
				"service-A.default.svc.cluster.local:7777": {
					"service-A.default.svc.cluster.local":      true,
					"service-A.default.svc.cluster.local:7777": true,
				},
				"service-A.v2:7777": {
					"service-A.v2":      true,
					"service-A.v2:7777": true,
				},
				"service-A.v3:7777": {
					"service-A.v3":      true,
					"service-A.v3:7777": true,
				},
			},
			expectedRoutes: 7,
			registryOnly:   false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testSidecarRDSVHosts(t, services, c.sidecarConfig, c.virtualServiceConfigs,
				c.routeName, c.expectedHosts, c.expectedRoutes, c.registryOnly)
		})
	}
}

func TestSelectVirtualService(t *testing.T) {
	services := []*model.Service{
		buildHTTPService("bookinfo.com", visibility.Public, wildcardIP, "default", 9999, 70),
		buildHTTPService("private.com", visibility.Private, wildcardIP, "default", 9999, 80),
		buildHTTPService("test.com", visibility.Public, "8.8.8.8", "not-default", 8080),
		buildHTTPService("test-private.com", visibility.Private, "9.9.9.9", "not-default", 80, 70),
		buildHTTPService("test-private-2.com", visibility.Private, "9.9.9.10", "not-default", 60),
		buildHTTPService("test-headless.com", visibility.Public, wildcardIP, "not-default", 8888),
	}

	servicesByName := make(map[host.Name]*model.Service, len(services))
	for _, svc := range services {
		servicesByName[svc.Hostname] = svc
	}

	virtualServiceSpec1 := &networking.VirtualService{
		Hosts:    []string{"test-private-2.com"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							// Subset: "some-subset",
							Host: "example.org",
							Port: &networking.PortSelector{
								Number: 61,
							},
						},
						Weight: 100,
					},
				},
			},
		},
	}
	virtualServiceSpec2 := &networking.VirtualService{
		Hosts:    []string{"test-private-2.com"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "test.org",
							Port: &networking.PortSelector{
								Number: 62,
							},
						},
						Weight: 100,
					},
				},
			},
		},
	}
	virtualServiceSpec3 := &networking.VirtualService{
		Hosts:    []string{"test-private-3.com"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "test.org",
							Port: &networking.PortSelector{
								Number: 63,
							},
						},
						Weight: 100,
					},
				},
			},
		},
	}
	virtualServiceSpec4 := &networking.VirtualService{
		Hosts:    []string{"test-headless.com", "example.com"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "test.org",
							Port: &networking.PortSelector{
								Number: 64,
							},
						},
						Weight: 100,
					},
				},
			},
		},
	}
	virtualServiceSpec5 := &networking.VirtualService{
		Hosts:    []string{"test-svc.testns.svc.cluster.local"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "test-svc.testn.svc.cluster.local",
						},
						Weight: 100,
					},
				},
			},
		},
	}
	virtualServiceSpec6 := &networking.VirtualService{
		Hosts:    []string{"match-no-service"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "non-exist-service",
						},
						Weight: 100,
					},
				},
			},
		},
	}
	virtualService1 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme2-v1",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec1,
	}
	virtualService2 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme-v2",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec2,
	}
	virtualService3 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme-v3",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec3,
	}
	virtualService4 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme-v4",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec4,
	}
	virtualService5 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme-v3",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec5,
	}
	virtualService6 := config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme-v3",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec6,
	}
	configs := selectVirtualServices(
		[]config.Config{virtualService1, virtualService2, virtualService3, virtualService4, virtualService5, virtualService6},
		servicesByName)
	expectedVS := []string{virtualService1.Name, virtualService2.Name, virtualService4.Name}
	if len(expectedVS) != len(configs) {
		t.Fatalf("Unexpected virtualService, got %d, epxected %d", len(configs), len(expectedVS))
	}
	for i, config := range configs {
		if config.Name != expectedVS[i] {
			t.Fatalf("Unexpected virtualService, got %s, epxected %s", config.Name, expectedVS[i])
		}
	}
}

func testSidecarRDSVHosts(t *testing.T, services []*model.Service,
	sidecarConfig *config.Config, virtualServices []*config.Config, routeName string,
	expectedHosts map[string]map[string]bool, expectedRoutes int, registryOnly bool,
) {
	m := mesh.DefaultMeshConfig()
	if registryOnly {
		m.OutboundTrafficPolicy = &meshapi.MeshConfig_OutboundTrafficPolicy{Mode: meshapi.MeshConfig_OutboundTrafficPolicy_REGISTRY_ONLY}
	}
	cg := NewConfigGenTest(t, TestOptions{
		MeshConfig:     m,
		Services:       services,
		ConfigPointers: append(virtualServices, sidecarConfig),
	})

	proxy := &model.Proxy{ConfigNamespace: "not-default", DNSDomain: "default.example.org"}
	vHostCache := make(map[int][]*route.VirtualHost)
	resource, _ := cg.ConfigGen.buildSidecarOutboundHTTPRouteConfig(
		cg.SetupProxy(proxy), &model.PushRequest{Push: cg.PushContext()}, routeName, vHostCache, nil, nil)
	routeCfg := &route.RouteConfiguration{}
	resource.Resource.UnmarshalTo(routeCfg)
	xdstest.ValidateRouteConfiguration(t, routeCfg)
	if routeCfg == nil {
		t.Fatalf("got nil route for %s", routeName)
	}

	if expectedRoutes == 0 {
		expectedRoutes = len(expectedHosts)
	}
	numberOfRoutes := 0
	for _, vhost := range routeCfg.VirtualHosts {
		numberOfRoutes += len(vhost.Routes)
		if _, found := expectedHosts[vhost.Name]; !found {
			t.Fatalf("unexpected vhost block %s for route %s",
				vhost.Name, routeName)
		}

		for _, domain := range vhost.Domains {
			if !expectedHosts[vhost.Name][domain] {
				t.Fatalf("unexpected vhost domain %s in vhost %s, for route %s", domain, vhost.Name, routeName)
			}
		}
		for want := range expectedHosts[vhost.Name] {
			found := false
			for _, got := range vhost.Domains {
				if got == want {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected vhost domain %s in vhost %s, for route %s not found. got domains %v", want, vhost.Name, routeName, vhost.Domains)
			}
		}

		if !vhost.GetIncludeRequestAttemptCount() {
			t.Fatal("Expected that include request attempt count is set to true, but set to false")
		}
	}
	if (expectedRoutes >= 0) && (numberOfRoutes != expectedRoutes) {
		t.Errorf("Wrong number of routes. expected: %v, Got: %v", expectedRoutes, numberOfRoutes)
	}
}

func buildHTTPService(hostname string, v visibility.Instance, ip, namespace string, ports ...int) *model.Service {
	service := &model.Service{
		CreationTime:   tnow,
		Hostname:       host.Name(hostname),
		DefaultAddress: ip,
		Resolution:     model.DNSLB,
		Attributes: model.ServiceAttributes{
			ServiceRegistry: provider.Kubernetes,
			Namespace:       namespace,
			ExportTo:        map[visibility.Instance]bool{v: true},
		},
	}
	if ip == wildcardIP {
		service.Resolution = model.Passthrough
	}

	Ports := make([]*model.Port, 0)

	for _, p := range ports {
		Ports = append(Ports, &model.Port{
			Name:     fmt.Sprintf("http-%d", p),
			Port:     p,
			Protocol: protocol.HTTP,
		})
	}

	service.Ports = Ports
	return service
}
