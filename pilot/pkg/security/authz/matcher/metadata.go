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
	matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
)

// MetadataStringMatcher creates a metadata string matcher for the given filter, key and the
// string matcher.
func MetadataStringMatcher(filter, key string, m *matcherpb.StringMatcher) *matcherpb.MetadataMatcher {
	return &matcherpb.MetadataMatcher{
		Filter: filter,
		Path: []*matcherpb.MetadataMatcher_PathSegment{
			{
				Segment: &matcherpb.MetadataMatcher_PathSegment_Key{
					Key: key,
				},
			},
		},
		Value: &matcherpb.ValueMatcher{
			MatchPattern: &matcherpb.ValueMatcher_StringMatch{
				StringMatch: m,
			},
		},
	}
}

// MetadataListMatcher creates a metadata list matcher for the given path keys and value.
func MetadataListMatcher(filter string, keys []string, value string) *matcherpb.MetadataMatcher {
	listMatcher := &matcherpb.ListMatcher{
		MatchPattern: &matcherpb.ListMatcher_OneOf{
			OneOf: &matcherpb.ValueMatcher{
				MatchPattern: &matcherpb.ValueMatcher_StringMatch{
					StringMatch: StringMatcher(value),
				},
			},
		},
	}

	paths := make([]*matcherpb.MetadataMatcher_PathSegment, 0)
	for _, k := range keys {
		paths = append(paths, &matcherpb.MetadataMatcher_PathSegment{
			Segment: &matcherpb.MetadataMatcher_PathSegment_Key{
				Key: k,
			},
		})
	}

	return &matcherpb.MetadataMatcher{
		Filter: filter,
		Path:   paths,
		Value: &matcherpb.ValueMatcher{
			MatchPattern: &matcherpb.ValueMatcher_ListMatch{
				ListMatch: listMatcher,
			},
		},
	}
}
