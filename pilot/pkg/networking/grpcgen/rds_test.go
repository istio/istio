// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package grpcgen

import (
	"strings"
	"testing"

	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/google/go-cmp/cmp"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
)

func TestBuildHTTPRoutes(t *testing.T) {
	services := []*model.Service{}
	for _, name := range []string{"foo", "bar"} {
		services = append(services, &model.Service{
			Hostname: host.Name(name + ".test.svc.cluster.local"),
			Attributes: model.ServiceAttributes{
				Name:      name,
				Namespace: "test",
				Aliases:   []model.NamespacedHostname{{Hostname: host.Name(name + "-alias.test.svc.cluster.local"), Namespace: "test"}},
			},
			Ports: model.PortList{
				{Name: "grpc", Port: 8080, Protocol: protocol.GRPC},
				{Name: "grpc-other", Port: 9090, Protocol: protocol.GRPC},
			},
		})
	}
	// Interleave ports and request foo again after bar to catch reuse of filtered hosts.
	names := []string{
		"outbound|8080||foo.test.svc.cluster.local",
		"outbound|9090||bar.test.svc.cluster.local",
		"outbound|8080||bar.test.svc.cluster.local",
		"outbound|9090||foo.test.svc.cluster.local",
		"outbound|8080|v1|foo.test.svc.cluster.local",
		"outbound|8080||missing.test.svc.cluster.local",
		"outbound|7070||foo.test.svc.cluster.local",
		"outbound|7070||bar.test.svc.cluster.local",
		"outbound|8080||foo-alias.test.svc.cluster.local",
	}
	g := &GrpcConfigGenerator{}
	for _, tt := range []struct {
		name    string
		sidecar string
		want    []string
	}{
		{
			name: "default scope",
			want: []string{"foo:8080", "bar:9090", "bar:8080", "foo:9090", "foo:8080", "", "", "", "foo:8080"},
		},
		{
			name: "port scoped listeners",
			sidecar: `
apiVersion: networking.istio.io/v1
kind: Sidecar
metadata: {name: scoped, namespace: test}
spec:
  egress:
  - port: {number: 8080, name: grpc, protocol: GRPC}
    hosts: ["test/foo.test.svc.cluster.local"]
  - port: {number: 9090, name: grpc-other, protocol: GRPC}
    hosts: ["test/bar.test.svc.cluster.local"]
`,
			want: []string{"foo:8080", "bar:9090", "", "", "foo:8080", "", "", "", ""},
		},
		{
			name: "HTTP proxy listener",
			sidecar: `
apiVersion: networking.istio.io/v1
kind: Sidecar
metadata: {name: proxy, namespace: test}
spec:
  egress:
  - port: {number: 8080, name: proxy, protocol: HTTP_PROXY}
    hosts: ["*/*"]
`,
			want: []string{"foo:8080", "", "bar:8080", "", "foo:8080", "", "", "", "foo:8080"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := core.NewConfigGenTest(t, core.TestOptions{Services: services, ConfigString: tt.sidecar})
			p := f.SetupProxy(&model.Proxy{ConfigNamespace: "test", Metadata: &model.NodeMetadata{Generator: "grpc"}})
			// Reuse the generator across different scopes to check that results stay local to each call.
			got := g.BuildHTTPRoutes(p, f.PushContext(), append(names, "invalid", "outbound|0||foo.test.svc.cluster.local"))
			if len(got) != len(names) {
				t.Fatalf("got %d routes, want %d", len(got), len(names))
			}
			for i, resource := range got {
				rc := &route.RouteConfiguration{}
				if err := resource.Resource.UnmarshalTo(rc); err != nil {
					t.Fatal(err)
				}
				if resource.Name != names[i] || rc.Name != names[i] {
					t.Fatalf("got resource %q and route %q, want %q", resource.Name, rc.Name, names[i])
				}
				if tt.want[i] == "" {
					if len(rc.VirtualHosts) != 0 {
						t.Errorf("%s: expected no virtual hosts, got %v", names[i], rc.VirtualHosts)
					}
					continue
				}
				wantName := strings.Replace(tt.want[i], ":", ".test.svc.cluster.local:", 1)
				if len(rc.VirtualHosts) != 1 || rc.VirtualHosts[0].Name != wantName {
					t.Fatalf("%s: expected virtual host %s, got %v", names[i], wantName, rc.VirtualHosts)
				}
				hostname, port, _ := strings.Cut(tt.want[i], ":")
				wantCluster := "outbound|" + port + "||" + hostname + ".test.svc.cluster.local"
				if routes := rc.VirtualHosts[0].Routes; len(routes) != 1 || routes[0].GetRoute().GetCluster() != wantCluster {
					t.Errorf("%s: expected route to %s, got %v", names[i], wantCluster, routes)
				}
			}
		})
	}
}

func TestFilterVirtualHostsForHostname(t *testing.T) {
	t.Parallel()

	const service = "svc-a.test.svc.cluster.local"
	const port = 8080

	tests := []struct {
		name         string
		virtualHosts []*route.VirtualHost
		hostname     string
		port         int
		wantNames    []string
	}{
		{
			name: "exact host domain",
			virtualHosts: []*route.VirtualHost{
				{Name: "svc-a", Domains: []string{service}},
				{Name: "svc-b", Domains: []string{"svc-b.test.svc.cluster.local"}},
			},
			hostname:  service,
			port:      port,
			wantNames: []string{"svc-a"},
		},
		{
			name: "exact hostport domain",
			virtualHosts: []*route.VirtualHost{
				{Name: "svc-a", Domains: []string{service + ":8080"}},
				{Name: "wrong-port", Domains: []string{service + ":9090"}},
				{Name: "svc-b", Domains: []string{"svc-b.test.svc.cluster.local:8080"}},
			},
			hostname:  service,
			port:      port,
			wantNames: []string{"svc-a"},
		},
		{
			name: "wildcard domain",
			virtualHosts: []*route.VirtualHost{
				{Name: "wildcard", Domains: []string{"*.test.svc.cluster.local"}},
				{Name: "other", Domains: []string{"*.other.svc.cluster.local"}},
			},
			hostname:  service,
			port:      port,
			wantNames: []string{"wildcard"},
		},
		{
			name: "wildcard hostport domain",
			virtualHosts: []*route.VirtualHost{
				{Name: "wildcard", Domains: []string{"*.test.svc.cluster.local:8080"}},
				{Name: "wrong-port", Domains: []string{"*.test.svc.cluster.local:9090"}},
			},
			hostname:  service,
			port:      port,
			wantNames: []string{"wildcard"},
		},
		{
			name: "ipv6 exact host and hostport",
			virtualHosts: []*route.VirtualHost{
				{Name: "ipv6-host", Domains: []string{"[2001:db8::1]"}},
				{Name: "ipv6-hostport", Domains: []string{"[2001:db8::1]:8080"}},
				{Name: "other", Domains: []string{"[2001:db8::2]:8080"}},
			},
			hostname:  "2001:db8::1",
			port:      port,
			wantNames: []string{"ipv6-host", "ipv6-hostport"},
		},
		{
			name: "domain matching is case insensitive",
			virtualHosts: []*route.VirtualHost{
				{Name: "svc-a", Domains: []string{"SVC-A.TEST.SVC.CLUSTER.LOCAL"}},
				{Name: "svc-b", Domains: []string{"SVC-B.TEST.SVC.CLUSTER.LOCAL"}},
			},
			hostname:  service,
			port:      port,
			wantNames: []string{"svc-a"},
		},
		{
			name: "no match",
			virtualHosts: []*route.VirtualHost{
				{Name: "svc-b", Domains: []string{"svc-b.test.svc.cluster.local"}},
			},
			hostname: service,
			port:     port,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := filterVirtualHostsForHostname(test.virtualHosts, test.hostname, test.port)
			var gotNames []string
			for _, vh := range got {
				gotNames = append(gotNames, vh.Name)
			}

			if diff := cmp.Diff(test.wantNames, gotNames); diff != "" {
				t.Fatalf("unexpected filtered virtual hosts (-want +got):\n%s", diff)
			}
		})
	}
}
