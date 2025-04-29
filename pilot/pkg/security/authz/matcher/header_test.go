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

package matcher

import (
	"testing"

	routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
)

func TestHeaderMatcher(t *testing.T) {
	testCases := []struct {
		Name   string
		K      string
		V      string
		Expect *routepb.HeaderMatcher
	}{
		{
			Name: "exact match",
			K:    ":path",
			V:    "/productpage",
			Expect: &routepb.HeaderMatcher{
				Name: ":path",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_StringMatch{
					StringMatch: StringMatcherExact("/productpage", false),
				},
			},
		},
		{
			Name: "suffix match",
			K:    ":path",
			V:    "*/productpage*",
			Expect: &routepb.HeaderMatcher{
				Name: ":path",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_StringMatch{
					StringMatch: StringMatcherSuffix("/productpage*", false),
				},
			},
		},
		{
			Name: "prefix match",
			K:    ":path",
			V:    "/productpage*",
			Expect: &routepb.HeaderMatcher{
				Name: ":path",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_StringMatch{
					StringMatch: StringMatcherPrefix("/productpage", false),
				},
			},
		},
		{
			Name: "* match",
			K:    ":path",
			V:    "*",
			Expect: &routepb.HeaderMatcher{
				Name: ":path",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_PresentMatch{
					PresentMatch: true,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			actual := HeaderMatcher(tc.K, tc.V)
			if !cmp.Equal(tc.Expect, actual, protocmp.Transform()) {
				t.Errorf("expecting %v, but got %v", tc.Expect, actual)
			}
		})
	}
}

func TestInlineHeaderMatcher(t *testing.T) {
	testCases := []struct {
		Name   string
		K      string
		V      string
		Expect *routepb.HeaderMatcher
	}{
		{
			Name: "exact match",
			K:    ":path",
			V:    "/productpage",
			Expect: &routepb.HeaderMatcher{
				Name: ":path",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_SafeRegexMatch{
					SafeRegexMatch: &matcher.RegexMatcher{
						Regex: "^/productpage$|^/productpage,.*|.*,/productpage,.*|.*,/productpage$",
					},
				},
			},
		},
		{
			Name: "exact match with whitespace around",
			K:    ":path",
			V:    " /productpage ",
			Expect: &routepb.HeaderMatcher{
				Name: ":path",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_SafeRegexMatch{
					SafeRegexMatch: &matcher.RegexMatcher{
						Regex: "^ /productpage $|^ /productpage ,.*|.*, /productpage ,.*|.*, /productpage $",
					},
				},
			},
		},
		{
			Name: "suffix match",
			K:    ":path",
			V:    "*/productpage*",
			Expect: &routepb.HeaderMatcher{
				Name: ":path",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_SafeRegexMatch{
					SafeRegexMatch: &matcher.RegexMatcher{
						Regex: `^.*/productpage\*$|^.*/productpage\*,.*|.*,.*/productpage\*,.*|.*,.*/productpage\*$`,
					},
				},
			},
		},
		{
			Name: "suffix match with whitespace behind",
			K:    ":path",
			V:    "*/productpage* ",
			Expect: &routepb.HeaderMatcher{
				Name: ":path",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_SafeRegexMatch{
					SafeRegexMatch: &matcher.RegexMatcher{
						Regex: `^.*/productpage\* $|^.*/productpage\* ,.*|.*,.*/productpage\* ,.*|.*,.*/productpage\* $`,
					},
				},
			},
		},
		{
			Name: "prefix match",
			K:    ":path",
			V:    "/productpage*",
			Expect: &routepb.HeaderMatcher{
				Name: ":path",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_SafeRegexMatch{
					SafeRegexMatch: &matcher.RegexMatcher{
						Regex: `^/productpage.*$|^/productpage.*,.*|.*,/productpage.*,.*|.*,/productpage.*$`,
					},
				},
			},
		},
		{
			Name: "prefix match with whitespace before",
			K:    ":path",
			V:    " /productpage*",
			Expect: &routepb.HeaderMatcher{
				Name: ":path",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_SafeRegexMatch{
					SafeRegexMatch: &matcher.RegexMatcher{
						Regex: `^ /productpage.*$|^ /productpage.*,.*|.*, /productpage.*,.*|.*, /productpage.*$`,
					},
				},
			},
		},
		{
			Name: "present match",
			K:    ":path",
			V:    "*",
			Expect: &routepb.HeaderMatcher{
				Name: ":path",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_PresentMatch{
					PresentMatch: true,
				},
			},
		},
	}

	for _, tc := range testCases {
		actual := InlineHeaderMatcher(tc.K, tc.V)
		if !cmp.Equal(tc.Expect, actual, protocmp.Transform()) {
			t.Errorf("expecting %v, but got %v", tc.Expect, actual)
		}
	}
}

func TestHostMatcher(t *testing.T) {
	testCases := []struct {
		Name   string
		K      string
		V      string
		Expect *routepb.HeaderMatcher
	}{
		{
			Name: "present match",
			K:    ":authority",
			V:    "*",
			Expect: &routepb.HeaderMatcher{
				Name: ":authority",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_PresentMatch{
					PresentMatch: true,
				},
			},
		},
		{
			Name: "suffix match",
			K:    ":authority",
			V:    "*.example.com",
			Expect: &routepb.HeaderMatcher{
				Name: ":authority",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_StringMatch{
					StringMatch: StringMatcherSuffix(".example.com", true),
				},
			},
		},
		{
			Name: "prefix match",
			K:    ":authority",
			V:    "example.*",
			Expect: &routepb.HeaderMatcher{
				Name: ":authority",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_StringMatch{
					StringMatch: StringMatcherPrefix("example.", true),
				},
			},
		},
		{
			Name: "exact match",
			K:    ":authority",
			V:    "example.com",
			Expect: &routepb.HeaderMatcher{
				Name: ":authority",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_StringMatch{
					StringMatch: StringMatcherExact("example.com", true),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			actual := HostMatcher(tc.K, tc.V)
			if !cmp.Equal(tc.Expect, actual, protocmp.Transform()) {
				t.Errorf("expecting %v, but got %v", tc.Expect, actual)
			}
		})
	}
}

func TestPathMatcher(t *testing.T) {
	testCases := []struct {
		Name   string
		V      string
		Expect *matcher.PathMatcher
	}{
		{
			Name: "exact match",
			V:    "/productpage",
			Expect: &matcher.PathMatcher{
				Rule: &matcher.PathMatcher_Path{
					Path: StringMatcherExact("/productpage", false),
				},
			},
		},
		{
			Name: "prefix match",
			V:    "/prefix*",
			Expect: &matcher.PathMatcher{
				Rule: &matcher.PathMatcher_Path{
					Path: StringMatcherPrefix("/prefix", false),
				},
			},
		},
		{
			Name: "suffix match",
			V:    "*suffix",
			Expect: &matcher.PathMatcher{
				Rule: &matcher.PathMatcher_Path{
					Path: StringMatcherSuffix("suffix", false),
				},
			},
		},
		{
			Name: "wildcard match",
			V:    "*",
			Expect: &matcher.PathMatcher{
				Rule: &matcher.PathMatcher_Path{
					Path: StringMatcherRegex(".+"),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			actual := PathMatcher(tc.V)
			if !cmp.Equal(tc.Expect, actual, protocmp.Transform()) {
				t.Errorf("expecting %v, but got %v", tc.Expect, actual)
			}
		})
	}
}
