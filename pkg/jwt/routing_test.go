// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package jwt

import (
	"reflect"
	"testing"
)

func TestToRoutingClaim(t *testing.T) {
	cases := []struct {
		name string
		want RoutingClaim
	}{
		{
			name: "@request.auth.claims",
			want: RoutingClaim{},
		},
		{
			name: "@request.auth.claims-",
			want: RoutingClaim{},
		},
		{
			name: "request.auth.claims.",
			want: RoutingClaim{},
		},
		{
			name: "@request.auth.claims.",
			want: RoutingClaim{},
		},
		{
			name: "@request.auth.claims-abc",
			want: RoutingClaim{},
		},
		{
			name: "x-some-other-header",
			want: RoutingClaim{},
		},
		{
			name: "@request.auth.claims.key1",
			want: RoutingClaim{Match: true, Separator: Dot, Claims: []string{"key1"}},
		},
		{
			name: "@request.auth.claims.key1.KEY2",
			want: RoutingClaim{Match: true, Separator: Dot, Claims: []string{"key1", "KEY2"}},
		},
		{
			name: "@request.auth.claims.key1-key2",
			want: RoutingClaim{Match: true, Separator: Dot, Claims: []string{"key1-key2"}},
		},
		{
			name: "@request.auth.claims[]",
			want: RoutingClaim{},
		},
		{
			name: "@request.auth.claims[key1",
			want: RoutingClaim{},
		},
		{
			name: "@request.auth.claims]key1",
			want: RoutingClaim{},
		},
		{
			// if `.` exists, use `.` as separator
			name: "@request.auth.claims.[key1]",
			want: RoutingClaim{Match: true, Separator: Dot, Claims: []string{"[key1]"}},
		},
		{
			name: "@request.auth.claims[key1]",
			want: RoutingClaim{Match: true, Separator: Square, Claims: []string{"key1"}},
		},
		{
			name: "@request.auth.claims[key1][key2]",
			want: RoutingClaim{Match: true, Separator: Square, Claims: []string{"key1", "key2"}},
		},
		{
			name: "@request.auth.claims[key1][KEY2]",
			want: RoutingClaim{Match: true, Separator: Square, Claims: []string{"key1", "KEY2"}},
		},
		{
			name: "@request.auth.claims[key1] [key2]",
			want: RoutingClaim{Match: true, Separator: Square, Claims: []string{"key1] [key2"}},
		},
		{
			name: "@request.auth.claims[test-issuer-2@istio.io]",
			want: RoutingClaim{Match: true, Separator: Square, Claims: []string{"test-issuer-2@istio.io"}},
		},
		{
			name: "@request.auth.claims[test-issuer-2@istio.io][key1]",
			want: RoutingClaim{Match: true, Separator: Square, Claims: []string{"test-issuer-2@istio.io", "key1"}},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := ToRoutingClaim(tt.name)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("want %v, but got %v", tt.want, got)
			}
		})
	}
}
