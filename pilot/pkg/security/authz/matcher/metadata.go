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
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
)

// MetadataStringMatcher creates a metadata string matcher for the given filter, key and the
// string matcher.
func MetadataStringMatcher(filter, key string, m *matcher.StringMatcher) *matcher.MetadataMatcher {
	return MetadataValueMatcher(filter, key, &matcher.ValueMatcher{
		MatchPattern: &matcher.ValueMatcher_StringMatch{
			StringMatch: m,
		},
	})
}

// MetadataValueMatcher creates a metadata value matcher for the given filter, key and the
// string matcher.
func MetadataValueMatcher(filter, key string, m *matcher.ValueMatcher) *matcher.MetadataMatcher {
	return &matcher.MetadataMatcher{
		Filter: filter,
		Path: []*matcher.MetadataMatcher_PathSegment{
			{
				Segment: &matcher.MetadataMatcher_PathSegment_Key{
					Key: key,
				},
			},
		},
		Value: m,
	}
}

// MetadataListMatcher creates a metadata list matcher for the given path keys and value.
func MetadataListMatcher(filter string, keys []string, value *matcher.StringMatcher, useExtendedJwt bool) *matcher.MetadataMatcher {
	return MetadataListValueMatcher(filter, keys, &matcher.ValueMatcher{
		MatchPattern: &matcher.ValueMatcher_StringMatch{
			StringMatch: value,
		},
	}, useExtendedJwt)
}

// MetadataListValueMatcher creates a metadata list matcher for the given path keys and value.
func MetadataListValueMatcher(filter string, keys []string, value *matcher.ValueMatcher, useExtendedJwt bool) *matcher.MetadataMatcher {
	listMatcher := &matcher.ListMatcher{
		MatchPattern: &matcher.ListMatcher_OneOf{
			OneOf: value,
		},
	}

	paths := make([]*matcher.MetadataMatcher_PathSegment, 0, len(keys))
	for _, k := range keys {
		paths = append(paths, &matcher.MetadataMatcher_PathSegment{
			Segment: &matcher.MetadataMatcher_PathSegment_Key{
				Key: k,
			},
		})
	}

	out := &matcher.MetadataMatcher{
		Filter: filter,
		Path:   paths,
	}
	if useExtendedJwt {
		out.Value = &matcher.ValueMatcher{
			MatchPattern: &matcher.ValueMatcher_OrMatch{
				OrMatch: &matcher.OrMatcher{
					ValueMatchers: []*matcher.ValueMatcher{
						{
							MatchPattern: &matcher.ValueMatcher_ListMatch{
								ListMatch: listMatcher,
							},
						},
						value,
					},
				},
			},
		}
	} else {
		out.Value = &matcher.ValueMatcher{
			MatchPattern: &matcher.ValueMatcher_ListMatch{
				ListMatch: listMatcher,
			},
		}
	}
	return out
}
