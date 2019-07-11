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
	envoy_matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher"
)

func MetadataStringMatcher(filter, key string, m *envoy_matcher.StringMatcher) *envoy_matcher.MetadataMatcher {
	return &envoy_matcher.MetadataMatcher{
		Filter: filter,
		Path: []*envoy_matcher.MetadataMatcher_PathSegment{
			{
				Segment: &envoy_matcher.MetadataMatcher_PathSegment_Key{
					Key: key,
				},
			},
		},
		Value: &envoy_matcher.ValueMatcher{
			MatchPattern: &envoy_matcher.ValueMatcher_StringMatch{
				StringMatch: m,
			},
		},
	}
}

// MetadataListMatcher generates a metadata list matcher for the given path keys and value.
func MetadataListMatcher(filter string, keys []string, value string) *envoy_matcher.MetadataMatcher {
	listMatcher := &envoy_matcher.ListMatcher{
		MatchPattern: &envoy_matcher.ListMatcher_OneOf{
			OneOf: &envoy_matcher.ValueMatcher{
				MatchPattern: &envoy_matcher.ValueMatcher_StringMatch{
					StringMatch: StringMatcher(value),
				},
			},
		},
	}

	paths := make([]*envoy_matcher.MetadataMatcher_PathSegment, 0)
	for _, k := range keys {
		paths = append(paths, &envoy_matcher.MetadataMatcher_PathSegment{
			Segment: &envoy_matcher.MetadataMatcher_PathSegment_Key{
				Key: k,
			},
		})
	}

	return &envoy_matcher.MetadataMatcher{
		Filter: filter,
		Path:   paths,
		Value: &envoy_matcher.ValueMatcher{
			MatchPattern: &envoy_matcher.ValueMatcher_ListMatch{
				ListMatch: listMatcher,
			},
		},
	}
}
