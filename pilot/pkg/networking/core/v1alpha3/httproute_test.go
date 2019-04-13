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
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
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

func TestSidecarOutboundHTTPRouteConfig(t *testing.T) {
	services := []*model.Service{
		buildHTTPService("bookinfo.com", model.VisibilityPublic, wildcardIP, "default", 9999, 70),
		buildHTTPService("private.com", model.VisibilityPrivate, wildcardIP, "default", 9999, 80),
		buildHTTPService("test.com", model.VisibilityPublic, "8.8.8.8", "not-default", 8080),
		buildHTTPService("test-private.com", model.VisibilityPrivate, "9.9.9.9", "not-default", 80, 70),
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

	// With the config above, RDS should return a valid route for the following route names
	// port 9000 - [bookinfo.com:9999], [bookinfo.com:70] but no bookinfo.com
	// unix://foo/bar/baz - [bookinfo.com:9999], [bookinfo.com:70] but no bookinfo.com
	// port 8080 - [bookinfo.com:9999], [bookinfo.com:70], [test.com:8080, 8.8.8.8:8080], but no bookinfo.com or test.com
	// port 9999 - [bookinfo.com, bookinfo.com:9999]
	// port 80 - [test-private.com, test-private.com:80, 9.9.9.9:80, 9.9.9.9]
	// port 70 - [test-private.com, test-private.com:70, 9.9.9.9, 9.9.9.9:70], [bookinfo.com, bookinfo.com:70]

	// Without sidecar config [same as wildcard egress listener], expect routes
	// 9999 - [bookinfo.com, bookinfo.com:9999],
	// 8080 - [test.com, test.com:8080, 8.8.8.8:8080, 8.8.8.8]
	// 80 - [test-private.com, test-private.com:80, 9.9.9.9:80, 9.9.9.9]
	// 70 - [bookinfo.com, bookinfo.com:70],[test-private.com, test-private.com:70, 9.9.9.9:70, 9.9.9.9]
	cases := []struct {
		name          string
		routeName     string
		sidecarConfig *model.Config
		// virtualHost Name and domains
		expectedHosts    map[string]map[string]bool
		fallthroughRoute bool
		registryOnly     bool
	}{
		{
			name:          "sidecar config port that is not in any service",
			routeName:     "9000",
			sidecarConfig: sidecarConfig,
			expectedHosts: map[string]map[string]bool{
				"bookinfo.com:9999": {"bookinfo.com:9999": true},
				"bookinfo.com:70":   {"bookinfo.com:70": true},
			},
		},
		{
			name:          "sidecar config with unix domain socket listener",
			routeName:     "unix://foo/bar/baz",
			sidecarConfig: sidecarConfig,
			expectedHosts: map[string]map[string]bool{
				"bookinfo.com:9999": {"bookinfo.com:9999": true},
				"bookinfo.com:70":   {"bookinfo.com:70": true},
			},
		},
		{
			name:          "sidecar config port that is in one of the services",
			routeName:     "8080",
			sidecarConfig: sidecarConfig,
			expectedHosts: map[string]map[string]bool{
				"bookinfo.com:9999": {"bookinfo.com:9999": true},
				"bookinfo.com:70":   {"bookinfo.com:70": true},
				"test.com:8080":     {"test.com:8080": true, "8.8.8.8:8080": true},
			},
		},
		{
			name:          "wildcard egress importing from all namespaces: 9999",
			routeName:     "9999",
			sidecarConfig: sidecarConfig,
			expectedHosts: map[string]map[string]bool{
				"bookinfo.com:9999": {"bookinfo.com:9999": true, "bookinfo.com": true},
			},
		},
		{
			name:          "wildcard egress importing from all namespaces: 80",
			routeName:     "80",
			sidecarConfig: sidecarConfig,
			expectedHosts: map[string]map[string]bool{
				"test-private.com:80": {
					"test-private.com": true, "test-private.com:80": true, "9.9.9.9": true, "9.9.9.9:80": true,
				},
			},
		},
		{
			name:          "wildcard egress importing from all namespaces: 70",
			routeName:     "70",
			sidecarConfig: sidecarConfig,
			expectedHosts: map[string]map[string]bool{
				"test-private.com:70": {
					"test-private.com": true, "test-private.com:70": true, "9.9.9.9": true, "9.9.9.9:70": true,
				},
				"bookinfo.com:70": {"bookinfo.com": true, "bookinfo.com:70": true},
			},
		},
		{
			name:          "no sidecar config - import public service from other namespaces: 9999",
			routeName:     "9999",
			sidecarConfig: nil,
			expectedHosts: map[string]map[string]bool{
				"bookinfo.com:9999": {"bookinfo.com:9999": true, "bookinfo.com": true},
			},
		},
		{
			name:          "no sidecar config - import public service from other namespaces: 8080",
			routeName:     "8080",
			sidecarConfig: nil,
			expectedHosts: map[string]map[string]bool{
				"test.com:8080": {
					"test.com:8080": true, "test.com": true, "8.8.8.8": true, "8.8.8.8:8080": true},
			},
		},
		{
			name:          "no sidecar config - import public services from other namespaces: 80",
			routeName:     "80",
			sidecarConfig: nil,
			expectedHosts: map[string]map[string]bool{
				"test-private.com:80": {
					"test-private.com": true, "test-private.com:80": true, "9.9.9.9": true, "9.9.9.9:80": true,
				},
			},
		},
		{
			name:          "no sidecar config - import public services from other namespaces: 70",
			routeName:     "70",
			sidecarConfig: nil,
			expectedHosts: map[string]map[string]bool{
				"test-private.com:70": {
					"test-private.com": true, "test-private.com:70": true, "9.9.9.9": true, "9.9.9.9:70": true,
				},
				"bookinfo.com:70": {"bookinfo.com": true, "bookinfo.com:70": true},
			},
		},
		{
			name:          "no sidecar config - import public services from other namespaces: 80 with fallthrough",
			routeName:     "80",
			sidecarConfig: nil,
			expectedHosts: map[string]map[string]bool{
				"test-private.com:80": {
					"test-private.com": true, "test-private.com:80": true, "9.9.9.9": true, "9.9.9.9:80": true,
				},
				"allow_any": {
					"*": true,
				},
			},
			fallthroughRoute: true,
		},
		{
			name:          "no sidecar config - import public services from other namespaces: 80 with fallthrough and registry only",
			routeName:     "80",
			sidecarConfig: nil,
			expectedHosts: map[string]map[string]bool{
				"test-private.com:80": {
					"test-private.com": true, "test-private.com:80": true, "9.9.9.9": true, "9.9.9.9:80": true,
				},
				"block_all": {
					"*": true,
				},
			},
			fallthroughRoute: true,
			registryOnly:     true,
		},
	}

	for _, c := range cases {
		testSidecarRDSVHosts(t, c.name, services, c.sidecarConfig, c.routeName, c.expectedHosts, c.fallthroughRoute, c.registryOnly)
	}
}

func testSidecarRDSVHosts(t *testing.T, testName string, services []*model.Service, sidecarConfig *model.Config,
	routeName string, expectedHosts map[string]map[string]bool, fallthroughRoute bool, registryOnly bool) {
	t.Helper()
	p := &fakePlugin{}
	configgen := NewConfigGenerator([]plugin.Plugin{p})

	env := buildListenerEnv(services)

	if err := env.PushContext.InitContext(&env); err != nil {
		t.Fatalf("testSidecarRDSVhosts(%s): failed to initialize push context", testName)
	}
	if sidecarConfig == nil {
		proxy.SidecarScope = model.DefaultSidecarScopeForNamespace(env.PushContext, "not-default")
	} else {
		proxy.SidecarScope = model.ConvertToSidecarScope(env.PushContext, sidecarConfig, sidecarConfig.Namespace)
	}
	os.Setenv("PILOT_ENABLE_FALLTHROUGH_ROUTE", "")
	if fallthroughRoute {
		os.Setenv("PILOT_ENABLE_FALLTHROUGH_ROUTE", "1")
	}
	if registryOnly {
		env.Mesh.OutboundTrafficPolicy = &meshconfig.MeshConfig_OutboundTrafficPolicy{Mode: meshconfig.MeshConfig_OutboundTrafficPolicy_REGISTRY_ONLY}
	}

	route := configgen.buildSidecarOutboundHTTPRouteConfig(&env, &proxy, env.PushContext, proxyInstances, routeName)
	if route == nil {
		t.Fatalf("testSidecarRDSVhosts(%s): got nil route for %s", testName, routeName)
	}

	for _, vhost := range route.VirtualHosts {
		if _, found := expectedHosts[vhost.Name]; !found {
			t.Fatalf("testSidecarRDSVhosts(%s): unexpected vhost block %s for route %s", testName,
				vhost.Name, routeName)
		}
		for _, domain := range vhost.Domains {
			if !expectedHosts[vhost.Name][domain] {
				t.Fatalf("testSidecarRDSVhosts(%s): unexpected vhost domain %s in vhost %s, for route %s",
					testName, domain, vhost.Name, routeName)
			}
		}
	}
}

func buildHTTPService(hostname string, visibility model.Visibility, ip, namespace string, ports ...int) *model.Service {
	service := &model.Service{
		CreationTime: tnow,
		Hostname:     model.Hostname(hostname),
		Address:      ip,
		ClusterVIPs:  make(map[string]string),
		Resolution:   model.Passthrough,
		Attributes: model.ServiceAttributes{
			Namespace: namespace,
			ExportTo:  map[model.Visibility]bool{visibility: true},
		},
	}

	Ports := make([]*model.Port, 0)

	for _, p := range ports {
		Ports = append(Ports, &model.Port{
			Name:     fmt.Sprintf("http-%d", p),
			Port:     p,
			Protocol: model.ProtocolHTTP,
		})
	}

	service.Ports = Ports
	return service
}
