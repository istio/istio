// Copyright 2019 Istio Authors
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
	"reflect"
	"testing"

	route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	envoy_matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher"
)

func TestHeaderMatcher(t *testing.T) {
	testCases := []struct {
		Name   string
		K      string
		V      string
		Expect *route.HeaderMatcher
	}{
		{
			Name: "exact match",
			K:    ":path",
			V:    "/productpage",
			Expect: &route.HeaderMatcher{
				Name: ":path",
				HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{
					ExactMatch: "/productpage",
				},
			},
		},
		{
			Name: "suffix match",
			K:    ":path",
			V:    "*/productpage*",
			Expect: &route.HeaderMatcher{
				Name: ":path",
				HeaderMatchSpecifier: &route.HeaderMatcher_SuffixMatch{
					SuffixMatch: "/productpage*",
				},
			},
		},
	}

	for _, tc := range testCases {
		actual := HeaderMatcher(tc.K, tc.V)
		if !reflect.DeepEqual(*tc.Expect, *actual) {
			t.Errorf("%s: expecting %v, but got %v", tc.Name, *tc.Expect, *actual)
		}
	}
}

func TestPathMatcher(t *testing.T) {
	testCases := []struct {
		Name   string
		V      string
		Expect *envoy_matcher.PathMatcher
	}{
		{
			Name: "exact match",
			V:    "/productpage",
			Expect: &envoy_matcher.PathMatcher{
				Rule: &envoy_matcher.PathMatcher_Path{
					Path: &envoy_matcher.StringMatcher{
						MatchPattern: &envoy_matcher.StringMatcher_Exact{
							Exact: "/productpage",
						},
					},
				},
			},
		},
		{
			Name: "prefix match",
			V:    "/prefix*",
			Expect: &envoy_matcher.PathMatcher{
				Rule: &envoy_matcher.PathMatcher_Path{
					Path: &envoy_matcher.StringMatcher{
						MatchPattern: &envoy_matcher.StringMatcher_Prefix{
							Prefix: "/prefix",
						},
					},
				},
			},
		},
		{
			Name: "suffix match",
			V:    "*suffix",
			Expect: &envoy_matcher.PathMatcher{
				Rule: &envoy_matcher.PathMatcher_Path{
					Path: &envoy_matcher.StringMatcher{
						MatchPattern: &envoy_matcher.StringMatcher_Suffix{
							Suffix: "suffix",
						},
					},
				},
			},
		},
		{
			Name: "wildcard match",
			V:    "*",
			Expect: &envoy_matcher.PathMatcher{
				Rule: &envoy_matcher.PathMatcher_Path{
					Path: &envoy_matcher.StringMatcher{
						MatchPattern: &envoy_matcher.StringMatcher_SafeRegex{
							SafeRegex: &envoy_matcher.RegexMatcher{
								Regex: ".+",
								EngineType: &envoy_matcher.RegexMatcher_GoogleRe2{
									GoogleRe2: &envoy_matcher.RegexMatcher_GoogleRE2{},
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
			if !reflect.DeepEqual(*tc.Expect, *actual) {
				t.Errorf("%s: expecting %v, but got %v", tc.Name, *tc.Expect, *actual)
			}
		})
	}
}
