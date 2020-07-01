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

	matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
)

func TestMetadataStringMatcher(t *testing.T) {
	matcher := &matcherpb.StringMatcher{
		MatchPattern: &matcherpb.StringMatcher_Exact{
			Exact: "exact",
		},
	}
	actual := MetadataStringMatcher("istio_authn", "key", matcher)
	expect := &matcherpb.MetadataMatcher{
		Filter: "istio_authn",
		Path: []*matcherpb.MetadataMatcher_PathSegment{
			{
				Segment: &matcherpb.MetadataMatcher_PathSegment_Key{
					Key: "key",
				},
			},
		},
		Value: &matcherpb.ValueMatcher{
			MatchPattern: &matcherpb.ValueMatcher_StringMatch{
				StringMatch: matcher,
			},
		},
	}

	if !cmp.Equal(actual, expect, protocmp.Transform()) {
		t.Errorf("want %s, got %s", expect.String(), actual.String())
	}
}

func TestMetadataListMatcher(t *testing.T) {
	getWant := func(regex string) *matcherpb.MetadataMatcher {
		return &matcherpb.MetadataMatcher{
			Filter: "istio_authn",
			Path: []*matcherpb.MetadataMatcher_PathSegment{
				{
					Segment: &matcherpb.MetadataMatcher_PathSegment_Key{
						Key: "key1",
					},
				},
				{
					Segment: &matcherpb.MetadataMatcher_PathSegment_Key{
						Key: "key2",
					},
				},
			},
			Value: &matcherpb.ValueMatcher{
				MatchPattern: &matcherpb.ValueMatcher_ListMatch{
					ListMatch: &matcherpb.ListMatcher{
						MatchPattern: &matcherpb.ListMatcher_OneOf{
							OneOf: &matcherpb.ValueMatcher{
								MatchPattern: &matcherpb.ValueMatcher_StringMatch{
									StringMatch: &matcherpb.StringMatcher{
										MatchPattern: &matcherpb.StringMatcher_SafeRegex{
											SafeRegex: &matcherpb.RegexMatcher{
												EngineType: &matcherpb.RegexMatcher_GoogleRe2{
													GoogleRe2: &matcherpb.RegexMatcher_GoogleRE2{},
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

	testCases := []struct {
		name string
		want string
	}{
		{
			name: "wildcard",
			want: ".+",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			want := getWant(tc.want)
			actual := MetadataListMatcher("istio_authn", []string{"key1", "key2"}, "*")
			if !cmp.Equal(want, actual, protocmp.Transform()) {
				t.Errorf("want %s, but got %s", want.String(), actual.String())
			}
		})
	}
}
