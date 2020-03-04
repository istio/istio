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
	"fmt"
	"reflect"
	"testing"

	envoy_matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher"
)

func TestMetadataStringMatcher(t *testing.T) {
	matcher := &envoy_matcher.StringMatcher{
		MatchPattern: &envoy_matcher.StringMatcher_Regex{
			Regex: "regex",
		},
	}
	actual := MetadataStringMatcher("istio_authn", "key", matcher)
	expect := &envoy_matcher.MetadataMatcher{
		Filter: "istio_authn",
		Path: []*envoy_matcher.MetadataMatcher_PathSegment{
			{
				Segment: &envoy_matcher.MetadataMatcher_PathSegment_Key{
					Key: "key",
				},
			},
		},
		Value: &envoy_matcher.ValueMatcher{
			MatchPattern: &envoy_matcher.ValueMatcher_StringMatch{
				StringMatch: matcher,
			},
		},
	}

	if !reflect.DeepEqual(*actual, *expect) {
		t.Errorf("want %s, got %s", expect.String(), actual.String())
	}
}

func TestMetadataListMatcher(t *testing.T) {
	getWant := func(regex string) envoy_matcher.MetadataMatcher {
		return envoy_matcher.MetadataMatcher{
			Filter: "istio_authn",
			Path: []*envoy_matcher.MetadataMatcher_PathSegment{
				{
					Segment: &envoy_matcher.MetadataMatcher_PathSegment_Key{
						Key: "key1",
					},
				},
				{
					Segment: &envoy_matcher.MetadataMatcher_PathSegment_Key{
						Key: "key2",
					},
				},
			},
			Value: &envoy_matcher.ValueMatcher{
				MatchPattern: &envoy_matcher.ValueMatcher_ListMatch{
					ListMatch: &envoy_matcher.ListMatcher{
						MatchPattern: &envoy_matcher.ListMatcher_OneOf{
							OneOf: &envoy_matcher.ValueMatcher{
								MatchPattern: &envoy_matcher.ValueMatcher_StringMatch{
									StringMatch: &envoy_matcher.StringMatcher{
										MatchPattern: &envoy_matcher.StringMatcher_SafeRegex{
											SafeRegex: &envoy_matcher.RegexMatcher{
												EngineType: &envoy_matcher.RegexMatcher_GoogleRe2{
													GoogleRe2: &envoy_matcher.RegexMatcher_GoogleRE2{},
												},
												Regex: regex,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}
	}
	getActual := func(treatWildcardAsRequired bool) envoy_matcher.MetadataMatcher {
		return *MetadataListMatcher("istio_authn", []string{"key1", "key2"}, "*", treatWildcardAsRequired)
	}

	testCases := []struct {
		treatWildcardAsRequired bool
		want                    string
	}{
		{
			treatWildcardAsRequired: false,
			want:                    ".*",
		},
		{
			treatWildcardAsRequired: true,
			want:                    ".+",
		},
	}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("treatWildcardAsRequired[%v]", tc.treatWildcardAsRequired), func(t *testing.T) {
			want := getWant(tc.want)
			actual := getActual(tc.treatWildcardAsRequired)
			if !reflect.DeepEqual(want, actual) {
				t.Errorf("want %s, but got %s", want.String(), actual.String())
			}
		})
	}
}
