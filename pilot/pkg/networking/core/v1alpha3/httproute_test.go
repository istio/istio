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
	"sort"
	"testing"

	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"

	meshapi "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collections"
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
			want: []string{"foo", "foo.local", "foo.local.campus", "foo.local.campus.net",
				"foo:80", "foo.local:80", "foo.local.campus:80", "foo.local.campus.net:80"},
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
			want: []string{"foo.local", "foo.local.campus", "foo.local.campus.net",
				"foo.local:80", "foo.local.campus:80", "foo.local.campus.net:80"},
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
	}

	for _, c := range cases {
		out := generateVirtualHostDomains(c.service, c.port, c.node)
		sort.SliceStable(c.want, func(i, j int) bool { return c.want[i] < c.want[j] })
		sort.SliceStable(out, func(i, j int) bool { return out[i] < out[j] })
		if !reflect.DeepEqual(out, c.want) {
			t.Errorf("buildVirtualHostDomains(%s): \ngot %v\n want %v", c.name, out, c.want)
		}
	}
}

func TestSidecarOutboundHTTPRouteConfigWithDuplicateHosts(t *testing.T) {
	services := []*model.Service{
		buildHTTPService("test-duplicate-domains.default.svc.cluster.local", visibility.Public, "172.10.10.19", "default", 7443, 70),
	}
	virtualServiceSpec6 := &networking.VirtualService{
		Hosts:    []string{"test-duplicate-domains.default.svc.cluster.local", "test-duplicate-domains.default"},
		Gateways: []string{"mesh"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "test-duplicate-domains.default.svc.cluster.local",
						},
					},
				},
			},
		},
	}

	virtualService6 := model.Config{
		ConfigMeta: model.ConfigMeta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme-v3",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec6,
	}

	virtualServices := []*model.Config{&virtualService6}
	p := &fakePlugin{}
	configgen := NewConfigGenerator([]plugin.Plugin{p})

	env := buildListenerEnvWithVirtualServices(services, virtualServices)

	if err := env.PushContext.InitContext(&env, nil, nil); err != nil {
		t.Fatalf("failed to initialize push context")
	}
	env.Mesh().OutboundTrafficPolicy = &meshapi.MeshConfig_OutboundTrafficPolicy{Mode: meshapi.MeshConfig_OutboundTrafficPolicy_REGISTRY_ONLY}

	proxy := model.Proxy{
		Type:        model.SidecarProxy,
		IPAddresses: []string{"1.1.1.1"},
		ID:          "v0.default",
		DNSDomain:   "svc.cluster.local",
		Metadata: &model.NodeMetadata{
			Namespace: "not-default",
		},
		ConfigNamespace: "not-default",
	}
	proxy.SidecarScope = model.DefaultSidecarScopeForNamespace(env.PushContext, "not-default")

	vHostCache := make(map[int][]*route.VirtualHost)
	routeName := "7443"
	routeCfg := configgen.buildSidecarOutboundHTTPRouteConfig(&proxy, env.PushContext, "7443", vHostCache)
	if routeCfg == nil {
		t.Fatalf("got nil route for %s", routeName)
	}

	if len(routeCfg.VirtualHosts) != 3 {
		t.Fatalf("unexpected virtual hosts %v", routeCfg.VirtualHosts)
	}

	vhosts := sets.Set{}
	domains := sets.Set{}
	for _, vhost := range routeCfg.VirtualHosts {
		if vhosts.Contains(vhost.Name) {
			t.Fatalf("duplicate virtual host found %s", vhost.Name)
		}
		vhosts.Insert(vhost.Name)
		for _, domain := range vhost.Domains {
			if domains.Contains(domain) {
				t.Fatalf("duplicate virtual host domain found %s", domain)
			}
			domains.Insert(domain)
		}
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
	}

	sidecarConfig := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
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
	sidecarConfigWithWildcard := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
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
	sidecarConfigWitHTTPProxy := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
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
	sidecarConfigWithRegistryOnly := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
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
	sidecarConfigWithAllowAny := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:      "foo",
			Namespace: "not-default",
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
							//Subset: "some-subset",
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
	virtualService1 := model.Config{
		ConfigMeta: model.ConfigMeta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme2-v1",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec1,
	}
	virtualService2 := model.Config{
		ConfigMeta: model.ConfigMeta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme-v2",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec2,
	}
	virtualService3 := model.Config{
		ConfigMeta: model.ConfigMeta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme-v3",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec3,
	}
	virtualService4 := model.Config{
		ConfigMeta: model.ConfigMeta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme-v4",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec4,
	}
	virtualService5 := model.Config{
		ConfigMeta: model.ConfigMeta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Virtualservices.Resource().GroupVersionKind(),
			Name:             "acme-v3",
			Namespace:        "not-default",
		},
		Spec: virtualServiceSpec5,
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
		sidecarConfig         *model.Config
		virtualServiceConfigs []*model.Config
		// virtualHost Name and domains
		expectedHosts map[string]map[string]bool
		registryOnly  bool
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
			name:                  "wildcard egress importing from all namespaces: 9999",
			routeName:             "9999",
			sidecarConfig:         sidecarConfig,
			virtualServiceConfigs: nil,
			expectedHosts: map[string]map[string]bool{
				"bookinfo.com:9999": {"bookinfo.com:9999": true, "bookinfo.com": true,
					"*.bookinfo.com:9999": true, "*.bookinfo.com": true},
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
				"bookinfo.com:70": {"bookinfo.com": true, "bookinfo.com:70": true,
					"*.bookinfo.com": true, "*.bookinfo.com:70": true},
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
				"bookinfo.com:9999": {"bookinfo.com:9999": true, "bookinfo.com": true,
					"*.bookinfo.com:9999": true, "*.bookinfo.com": true},
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
					"test.com:8080": true, "test.com": true, "8.8.8.8": true, "8.8.8.8:8080": true},
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
				"bookinfo.com:70": {"bookinfo.com": true, "bookinfo.com:70": true,
					"*.bookinfo.com": true, "*.bookinfo.com:70": true},
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
					"test-private.com": true, "test-private.com:70": true, "9.9.9.9": true, "9.9.9.9:70": true,
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
			virtualServiceConfigs: []*model.Config{&virtualService1, &virtualService2},
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
			virtualServiceConfigs: []*model.Config{&virtualService3},
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
			virtualServiceConfigs: []*model.Config{&virtualService4},
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
			virtualServiceConfigs: []*model.Config{&virtualService5},
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
			virtualServiceConfigs: []*model.Config{&virtualService5},
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
				"test-svc.testns.svc.cluster.local:80": {
					"test-svc.testns.svc.cluster.local": true, "test-svc.testns.svc.cluster.local:80": true,
				},
				"block_all": {
					"*": true,
				},
			},
			registryOnly: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testSidecarRDSVHosts(t, services, c.sidecarConfig, c.virtualServiceConfigs,
				c.routeName, c.expectedHosts, c.registryOnly)
		})
	}
}

func testSidecarRDSVHosts(t *testing.T, services []*model.Service,
	sidecarConfig *model.Config, virtualServices []*model.Config, routeName string,
	expectedHosts map[string]map[string]bool, registryOnly bool) {
	t.Helper()
	p := &fakePlugin{}
	configgen := NewConfigGenerator([]plugin.Plugin{p})

	env := buildListenerEnvWithVirtualServices(services, virtualServices)

	if err := env.PushContext.InitContext(&env, nil, nil); err != nil {
		t.Fatalf("failed to initialize push context")
	}
	if registryOnly {
		env.Mesh().OutboundTrafficPolicy = &meshapi.MeshConfig_OutboundTrafficPolicy{Mode: meshapi.MeshConfig_OutboundTrafficPolicy_REGISTRY_ONLY}
	}
	proxy := getProxy()
	if sidecarConfig == nil {
		proxy.SidecarScope = model.DefaultSidecarScopeForNamespace(env.PushContext, "not-default")
	} else {
		proxy.SidecarScope = model.ConvertToSidecarScope(env.PushContext, sidecarConfig, sidecarConfig.Namespace)
	}

	vHostCache := make(map[int][]*route.VirtualHost)
	routeCfg := configgen.buildSidecarOutboundHTTPRouteConfig(proxy, env.PushContext, routeName, vHostCache)
	if routeCfg == nil {
		t.Fatalf("got nil route for %s", routeName)
	}

	expectedNumberOfRoutes := len(expectedHosts)
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
	if (expectedNumberOfRoutes >= 0) && (numberOfRoutes != expectedNumberOfRoutes) {
		t.Errorf("Wrong number of routes. expected: %v, Got: %v", expectedNumberOfRoutes, numberOfRoutes)
	}
}

func buildHTTPService(hostname string, v visibility.Instance, ip, namespace string, ports ...int) *model.Service {
	service := &model.Service{
		CreationTime: tnow,
		Hostname:     host.Name(hostname),
		Address:      ip,
		ClusterVIPs:  make(map[string]string),
		Resolution:   model.DNSLB,
		Attributes: model.ServiceAttributes{
			ServiceRegistry: string(serviceregistry.Kubernetes),
			Namespace:       namespace,
			ExportTo:        map[visibility.Instance]bool{v: true},
		},
	}
	if service.Address == wildcardIP {
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
