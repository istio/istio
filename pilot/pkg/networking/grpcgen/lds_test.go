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

	"istio.io/istio/pkg/istio-agent/grpcxds"
)

var wildcardMap = map[string]struct{}{}

func TestListenerNameFilter(t *testing.T) {
	cases := map[string]struct {
		in          []string
		want        listenerNameFilter
		wantInbound []string
	}{
		"simple": {
			in: []string{"foo.com:80", "foo.com:443", "wildcard.com"},
			want: listenerNameFilter{
				"foo.com":      {"80": {}, "443": {}},
				"wildcard.com": wildcardMap,
			},
		},
		"plain-host clears port-map": {
			in:   []string{"foo.com:80", "foo.com"},
			want: listenerNameFilter{"foo.com": wildcardMap},
		},
		"port-map stays clear": {
			in:   []string{"foo.com:80", "foo.com", "foo.com:443"},
			want: listenerNameFilter{"foo.com": wildcardMap},
		},
		"special listeners preserved exactly": {
			in: []string{
				"foo.com:80",
				fmt.Sprintf(grpcxds.ServerListenerNameTemplate, "foo:1234"),
				fmt.Sprintf(grpcxds.ServerListenerNameTemplate, "foo"),
				fmt.Sprintf(grpcxds.ServerListenerNameTemplate, "[::]:8076"),
			},
			want: listenerNameFilter{
				"foo.com": {"80": {}},
				fmt.Sprintf(grpcxds.ServerListenerNameTemplate, "foo:1234"):  wildcardMap,
				fmt.Sprintf(grpcxds.ServerListenerNameTemplate, "foo"):       wildcardMap,
				fmt.Sprintf(grpcxds.ServerListenerNameTemplate, "[::]:8076"): wildcardMap,
			},
			wantInbound: []string{
				fmt.Sprintf(grpcxds.ServerListenerNameTemplate, "foo:1234"),
				fmt.Sprintf(grpcxds.ServerListenerNameTemplate, "foo"),
				fmt.Sprintf(grpcxds.ServerListenerNameTemplate, "[::]:8076"),
			},
		},
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			got := newListenerNameFilter(tt.in)
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
