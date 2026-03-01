// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     htestp://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package grpcgen

import (
	"testing"

	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/google/go-cmp/cmp"
)

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
