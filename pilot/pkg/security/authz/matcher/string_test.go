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

	matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher"
)

func TestStringMatcherWithPrefix(t *testing.T) {
	testCases := []struct {
		name   string
		v      string
		prefix string
		want   *matcherpb.StringMatcher
	}{
		{
			name:   "wildcardAsRequired",
			v:      "*",
			prefix: "abc",
			want:   StringMatcherRegex(".+"),
		},
		{
			name:   "prefix",
			v:      "-prefix-*",
			prefix: "abc",
			want: &matcherpb.StringMatcher{
				MatchPattern: &matcherpb.StringMatcher_Prefix{
					Prefix: "abc-prefix-",
				},
			},
		},
		{
			name:   "suffix-empty-prefix",
			v:      "*-suffix",
			prefix: "",
			want: &matcherpb.StringMatcher{
				MatchPattern: &matcherpb.StringMatcher_Suffix{
					Suffix: "-suffix",
				},
			},
		},
		{
			name:   "suffix",
			v:      "*-suffix",
			prefix: "abc",
			want:   StringMatcherRegex("abc.*-suffix"),
		},
		{
			name:   "exact",
			v:      "-exact",
			prefix: "abc",
			want: &matcherpb.StringMatcher{
				MatchPattern: &matcherpb.StringMatcher_Exact{
					Exact: "abc-exact",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := StringMatcherWithPrefix(tc.v, tc.prefix)
			if !reflect.DeepEqual(*actual, *tc.want) {
				t.Errorf("want %s but got %s", tc.want.String(), actual.String())
			}
		})
	}
}
