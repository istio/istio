package match

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

import (
	"testing"

	matcher "github.com/cncf/xds/go/xds/type/matcher/v3"
	"github.com/google/go-cmp/cmp"

	"istio.io/istio/pkg/util/protomarshal"
)

func TestCleanupEmptyMaps(t *testing.T) {
	tc := []struct {
		name  string
		given func() Mapper
		want  func() *matcher.Matcher
	}{
		{
			name: "empty map at depth = 0",
			given: func() Mapper {
				// root (dest port):
				//   <no matches>
				//   fallback (dest ip):
				//     1.2.3.4: chain
				fallback := NewDestinationIP()
				fallback.Map["1.2.3.4"] = ToChain("chain")

				root := NewDestinationPort()
				root.OnNoMatch = ToMatcher(fallback.Matcher)
				return root
			},
			want: func() *matcher.Matcher {
				// root (dest ip):
				//   1.2.3.4: chain

				// fallback becomes root
				want := NewDestinationIP()
				want.Map["1.2.3.4"] = ToChain("chain")
				return want.Matcher
			},
		},
		{
			name: "empty map at depth = 1",
			given: func() Mapper {
				// root (dest port)
				//   15001:
				//     inner (dest ip):
				//       <no matches>
				//       fallback: chain
				inner := NewDestinationIP()
				inner.OnNoMatch = ToChain("chain")

				root := NewDestinationPort()
				root.Map["15001"] = ToMatcher(inner.Matcher)
				return root
			},
			want: func() *matcher.Matcher {
				// dest port
				// 15001: chain
				want := NewDestinationPort()
				want.Map["15001"] = ToChain("chain")
				return want.Matcher
			},
		},
		{
			name: "empty map at depth = 0 and 1",
			given: func() Mapper {
				// root (dest port)
				//   fallback:
				//     inner (dest ip):
				//       <no matches>
				//       fallback: chain
				leaf := NewSourceIP()
				leaf.Map["1.2.3.4"] = ToChain("chain")

				inner := NewDestinationIP()
				inner.OnNoMatch = ToMatcher(leaf.Matcher)

				root := NewDestinationPort()
				root.OnNoMatch = ToMatcher(inner.Matcher)
				return root
			},
			want: func() *matcher.Matcher {
				// src port
				// 15001: chain
				want := NewSourceIP()
				want.Map["1.2.3.4"] = ToChain("chain")
				return want.Matcher
			},
		},
		{
			name: "empty map at depths = 1 and 2",
			given: func() Mapper {
				// root (dest port)
				//   15001:
				//     middle (dest ip):
				//       fallback:
				//         inner (src ip):
				//           fallback: chain
				inner := NewSourceIP()
				inner.OnNoMatch = ToChain("chain")

				middle := NewDestinationIP()
				middle.OnNoMatch = ToMatcher(inner.Matcher)

				root := NewDestinationPort()
				root.Map["15001"] = ToMatcher(middle.Matcher)
				return root
			},
			want: func() *matcher.Matcher {
				// dest port
				// 15001: chain
				want := NewDestinationPort()
				want.Map["15001"] = ToChain("chain")
				return want.Matcher
			},
		},
	}

	for _, tt := range tc {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.given().BuildMatcher()
			haveJSON, _ := protomarshal.ToJSONWithIndent(got, "  ")
			wantJSON, _ := protomarshal.ToJSONWithIndent(tt.want(), "  ")

			if diff := cmp.Diff(haveJSON, wantJSON, cmp.AllowUnexported()); diff != "" {
				t.Error(diff)
			}
		})
	}
}
