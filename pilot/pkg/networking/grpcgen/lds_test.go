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

package grpcgen

import (
	"fmt"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/istio-agent/grpcxds"
	"istio.io/istio/pkg/util/sets"
)

var node = &model.Proxy{DNSDomain: "ns.svc.cluster.local", Metadata: &model.NodeMetadata{Namespace: "ns"}}

func TestListenerNameFilter(t *testing.T) {
	cases := map[string]struct {
		in          []string
		want        listenerNames
		wantInbound []string
	}{
		"simple": {
			in: []string{"foo.com:80", "foo.com:443", "wildcard.com"},
			want: listenerNames{
				"foo.com": {
					RequestedNames: sets.NewWith("foo.com"),
					Ports:          sets.NewWith("80", "443"),
				},
				"wildcard.com": {RequestedNames: sets.NewWith("wildcard.com")},
			},
		},
		"plain-host clears port-map": {
			in:   []string{"foo.com:80", "foo.com"},
			want: listenerNames{"foo.com": {RequestedNames: sets.NewWith("foo.com")}},
		},
		"port-map stays clear": {
			in: []string{"foo.com:80", "foo.com", "foo.com:443"},
			want: listenerNames{"foo.com": {
				RequestedNames: sets.NewWith("foo.com"),
			}},
		},
		"special listeners preserved exactly": {
			in: []string{
				"foo.com:80",
				fmt.Sprintf(grpcxds.ServerListenerNameTemplate, "foo:1234"),
				fmt.Sprintf(grpcxds.ServerListenerNameTemplate, "foo"),
				fmt.Sprintf(grpcxds.ServerListenerNameTemplate, "[::]:8076"),
			},
			want: listenerNames{
				"foo.com": {
					RequestedNames: sets.NewWith("foo.com"),
					Ports:          sets.NewWith("80"),
				},
				fmt.Sprintf(grpcxds.ServerListenerNameTemplate, "foo:1234"): {
					RequestedNames: sets.NewWith(fmt.Sprintf(grpcxds.ServerListenerNameTemplate, "foo:1234")),
				},
				fmt.Sprintf(grpcxds.ServerListenerNameTemplate, "foo"): {
					RequestedNames: sets.NewWith(fmt.Sprintf(grpcxds.ServerListenerNameTemplate, "foo")),
				},
				fmt.Sprintf(grpcxds.ServerListenerNameTemplate, "[::]:8076"): {
					RequestedNames: sets.NewWith(fmt.Sprintf(grpcxds.ServerListenerNameTemplate, "[::]:8076")),
				},
			},
			wantInbound: []string{
				fmt.Sprintf(grpcxds.ServerListenerNameTemplate, "foo:1234"),
				fmt.Sprintf(grpcxds.ServerListenerNameTemplate, "foo"),
				fmt.Sprintf(grpcxds.ServerListenerNameTemplate, "[::]:8076"),
			},
		},
		"expand shortnames": {
			in: []string{
				"bar",
				"bar.ns",
				"bar.ns.svc",
				"bar.ns.svc.cluster.local",
				"foo:80",
				"foo.ns:81",
				"foo.ns.svc:82",
				"foo.ns.svc.cluster.local:83",
			},
			want: listenerNames{
				"bar":        {RequestedNames: sets.NewWith("bar")},
				"bar.ns":     {RequestedNames: sets.NewWith("bar.ns")},
				"bar.ns.svc": {RequestedNames: sets.NewWith("bar.ns.svc")},
				"bar.ns.svc.cluster.local": {RequestedNames: sets.NewWith(
					"bar",
					"bar.ns",
					"bar.ns.svc",
					"bar.ns.svc.cluster.local",
				)},
				"foo":        {RequestedNames: sets.NewWith("foo"), Ports: sets.NewWith("80")},
				"foo.ns":     {RequestedNames: sets.NewWith("foo.ns"), Ports: sets.NewWith("81")},
				"foo.ns.svc": {RequestedNames: sets.NewWith("foo.ns.svc"), Ports: sets.NewWith("82")},
				"foo.ns.svc.cluster.local": {
					RequestedNames: sets.NewWith(
						"foo",
						"foo.ns",
						"foo.ns.svc",
						"foo.ns.svc.cluster.local",
					),
					Ports: sets.NewWith("80", "81", "82", "83"),
				},
			},
		},
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			got := newListenerNameFilter(tt.in, node)
			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Fatal(diff)
			}
			gotInbound := got.inboundNames()
			sort.Strings(gotInbound)
			sort.Strings(tt.wantInbound)
			if diff := cmp.Diff(gotInbound, tt.wantInbound); diff != "" {
				t.Fatalf(diff)
			}
		})
	}
}

func TestTryFindFQDN(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"foo", "foo.ns.svc.cluster.local"},
		{"foo.ns", "foo.ns.svc.cluster.local"},
		{"foo.ns.svc", "foo.ns.svc.cluster.local"},
		{"foo.ns.svc.cluster.local", ""},
		{"foo.com", ""},
		{"foo.ns.com", ""},
		{"foo.ns.svc.notdnsdomain", ""},
		{"foo.ns.svc.cluster.local.extra", ""},
		{"xds.istio.io/grpc/lds/inbound/0.0.0.0:1234", ""},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := tryFindFQDN(tc.in, node); got != tc.want {
				t.Errorf("want %q but got %q", tc.want, got)
			}
		})
	}
}
