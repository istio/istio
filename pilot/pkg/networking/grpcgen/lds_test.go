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
	"testing"

	"github.com/google/go-cmp/cmp"
)

var wildcardMap = map[string]struct{}{}

func TestNewListenerNameFilter(t *testing.T) {
	cases := map[string]struct {
		in   []string
		want listenerNameFilter
	}{
		"simple": {
			in: []string{"foo.com:80", "foo.com:443", "wildcard.com"},
			want: listenerNameFilter{
				"foo.com":      map[string]struct{}{"80": {}, "443": {}},
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
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			got := newListenerNameFilter(tt.in)
			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}
