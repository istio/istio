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
	"regexp"
	"testing"

	routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
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
				HeaderMatchSpecifier: &routepb.HeaderMatcher_ExactMatch{
					ExactMatch: "/productpage",
				},
			},
		},
		{
			Name: "suffix match",
			K:    ":path",
			V:    "*/productpage*",
			Expect: &routepb.HeaderMatcher{
				Name: ":path",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_SuffixMatch{
					SuffixMatch: "/productpage*",
				},
			},
		},
		{
			Name: "prefix match",
			K:    ":path",
			V:    "/productpage*",
			Expect: &routepb.HeaderMatcher{
				Name: ":path",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_PrefixMatch{
					PrefixMatch: "/productpage",
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
		actual := HeaderMatcher(tc.K, tc.V)
		if !cmp.Equal(tc.Expect, actual, protocmp.Transform()) {
			t.Errorf("expecting %v, but got %v", tc.Expect, actual)
		}
	}
}

func TestHostMatcherWithRegex(t *testing.T) {
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
				Name:                 ":authority",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_PresentMatch{PresentMatch: true},
			},
		},
		{
			Name: "prefix match",
			K:    ":authority",
			V:    "*.example.com",
			Expect: &routepb.HeaderMatcher{
				Name: ":authority",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_SafeRegexMatch{
					SafeRegexMatch: &matcherpb.RegexMatcher{
						EngineType: &matcherpb.RegexMatcher_GoogleRe2{
							GoogleRe2: &matcherpb.RegexMatcher_GoogleRE2{},
						},
						Regex: `(?i).*\.example\.com`,
					},
				},
			},
		},
		{
			Name: "suffix match",
			K:    ":authority",
			V:    "example.*",
			Expect: &routepb.HeaderMatcher{
				Name: ":authority",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_SafeRegexMatch{
					SafeRegexMatch: &matcherpb.RegexMatcher{
						EngineType: &matcherpb.RegexMatcher_GoogleRe2{
							GoogleRe2: &matcherpb.RegexMatcher_GoogleRE2{},
						},
						Regex: `(?i)example\..*`,
					},
				},
			},
		},
		{
			Name: "exact match",
			K:    ":authority",
			V:    "example.com",
			Expect: &routepb.HeaderMatcher{
				Name: ":authority",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_SafeRegexMatch{
					SafeRegexMatch: &matcherpb.RegexMatcher{
						EngineType: &matcherpb.RegexMatcher_GoogleRe2{
							GoogleRe2: &matcherpb.RegexMatcher_GoogleRE2{},
						},
						Regex: `(?i)example\.com`,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			actual := HostMatcherWithRegex(tc.K, tc.V)
			// nolint: staticcheck
			// Update to not use the deprecated fields later.
			if re := actual.GetSafeRegexMatch().GetRegex(); re != "" {
				_, err := regexp.Compile(re)
				if err != nil {
					t.Errorf("failed to compile regex %s: %v", re, err)
				}
			}
			if !cmp.Equal(tc.Expect, actual, protocmp.Transform()) {
				t.Errorf("expecting %v, but got %v", tc.Expect, actual)
			}
		})
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
				Name:                 ":authority",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_PresentMatch{PresentMatch: true},
			},
		},
		{
			Name: "suffix match",
			K:    ":authority",
			V:    "*.example.com",
			Expect: &routepb.HeaderMatcher{
				Name: ":authority",
				HeaderMatchSpecifier: &routepb.HeaderMatcher_StringMatch{
					StringMatch: &matcherpb.StringMatcher{
						IgnoreCase: true,
						MatchPattern: &matcherpb.StringMatcher_Suffix{
							Suffix: ".example.com",
						},
					},
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
					StringMatch: &matcherpb.StringMatcher{
						IgnoreCase: true,
						MatchPattern: &matcherpb.StringMatcher_Prefix{
							Prefix: "example.",
						},
					},
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
					StringMatch: &matcherpb.StringMatcher{
						IgnoreCase: true,
						MatchPattern: &matcherpb.StringMatcher_Exact{
							Exact: "example.com",
						},
					},
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
		Expect *matcherpb.PathMatcher
	}{
		{
			Name: "exact match",
			V:    "/productpage",
			Expect: &matcherpb.PathMatcher{
				Rule: &matcherpb.PathMatcher_Path{
					Path: &matcherpb.StringMatcher{
						MatchPattern: &matcherpb.StringMatcher_Exact{
							Exact: "/productpage",
						},
					},
				},
			},
		},
		{
			Name: "prefix match",
			V:    "/prefix*",
			Expect: &matcherpb.PathMatcher{
				Rule: &matcherpb.PathMatcher_Path{
					Path: &matcherpb.StringMatcher{
						MatchPattern: &matcherpb.StringMatcher_Prefix{
							Prefix: "/prefix",
						},
					},
				},
			},
		},
		{
			Name: "suffix match",
			V:    "*suffix",
			Expect: &matcherpb.PathMatcher{
				Rule: &matcherpb.PathMatcher_Path{
					Path: &matcherpb.StringMatcher{
						MatchPattern: &matcherpb.StringMatcher_Suffix{
							Suffix: "suffix",
						},
					},
				},
			},
		},
		{
			Name: "wildcard match",
			V:    "*",
			Expect: &matcherpb.PathMatcher{
				Rule: &matcherpb.PathMatcher_Path{
					Path: &matcherpb.StringMatcher{
						MatchPattern: &matcherpb.StringMatcher_SafeRegex{
							SafeRegex: &matcherpb.RegexMatcher{
								Regex: ".+",
								EngineType: &matcherpb.RegexMatcher_GoogleRe2{
									GoogleRe2: &matcherpb.RegexMatcher_GoogleRE2{},
								},
							},
						},
					},
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
