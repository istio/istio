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

	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
)

type testCase struct {
	name   string
	v      string
	prefix string
	want   *matcher.StringMatcher
}

func TestStringMatcherWithPrefix(t *testing.T) {
	testCases := []testCase{
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
			want: &matcher.StringMatcher{
				MatchPattern: &matcher.StringMatcher_Prefix{
					Prefix: "abc-prefix-",
				},
			},
		},
		{
			name:   "suffix-empty-prefix",
			v:      "*-suffix",
			prefix: "",
			want: &matcher.StringMatcher{
				MatchPattern: &matcher.StringMatcher_Suffix{
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
			want: &matcher.StringMatcher{
				MatchPattern: &matcher.StringMatcher_Exact{
					Exact: "abc-exact",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := StringMatcherWithPrefix(tc.v, tc.prefix)
			if !cmp.Equal(actual, tc.want, protocmp.Transform()) {
				t.Errorf("want %s but got %s", tc.want.String(), actual.String())
			}
		})
	}
}

func TestStringMatcherRegex(t *testing.T) {
	testCases := []testCase{
		{
			name: "wildcardAsRequired",
			v:    "*",
			want: &matcher.StringMatcher{
				MatchPattern: &matcher.StringMatcher_SafeRegex{
					SafeRegex: &matcher.RegexMatcher{
						Regex: "*",
					},
				},
			},
		},
		{
			name: "regexExpression",
			v:    "+?",
			want: &matcher.StringMatcher{
				MatchPattern: &matcher.StringMatcher_SafeRegex{
					SafeRegex: &matcher.RegexMatcher{
						Regex: "+?",
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if actual := StringMatcherRegex(tc.v); !cmp.Equal(actual, tc.want, protocmp.Transform()) {
				t.Errorf("want %s but got %s", tc.want.String(), actual.String())
			}
		})
	}
}

func TestStringMatcherPrefix(t *testing.T) {
	testCases := []struct {
		name       string
		str        string
		ignoreCase bool
		want       *matcher.StringMatcher
	}{
		{
			name:       "prefix1",
			str:        "foo",
			ignoreCase: true,
			want: &matcher.StringMatcher{
				IgnoreCase: true,
				MatchPattern: &matcher.StringMatcher_Prefix{
					Prefix: "foo",
				},
			},
		},
		{
			name:       "prefix2",
			str:        "bar",
			ignoreCase: false,
			want: &matcher.StringMatcher{
				IgnoreCase: false,
				MatchPattern: &matcher.StringMatcher_Prefix{
					Prefix: "bar",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := StringMatcherPrefix(tc.str, tc.ignoreCase)
			if !cmp.Equal(got, tc.want, protocmp.Transform()) {
				t.Errorf("want %v but got %v", tc.want, got)
			}
		})
	}
}

func TestStringMatcherSuffix(t *testing.T) {
	testCases := []struct {
		name       string
		str        string
		ignoreCase bool
		want       *matcher.StringMatcher
	}{
		{
			name:       "suffix1",
			str:        "foo",
			ignoreCase: true,
			want: &matcher.StringMatcher{
				IgnoreCase: true,
				MatchPattern: &matcher.StringMatcher_Suffix{
					Suffix: "foo",
				},
			},
		},
		{
			name:       "suffix2",
			str:        "bar",
			ignoreCase: false,
			want: &matcher.StringMatcher{
				IgnoreCase: false,
				MatchPattern: &matcher.StringMatcher_Suffix{
					Suffix: "bar",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := StringMatcherSuffix(tc.str, tc.ignoreCase)
			if !cmp.Equal(got, tc.want, protocmp.Transform()) {
				t.Errorf("want %v but got %v", tc.want, got)
			}
		})
	}
}

func TestStringMatcherExact(t *testing.T) {
	testCases := []struct {
		name       string
		str        string
		ignoreCase bool
		want       *matcher.StringMatcher
	}{
		{
			name:       "exact1",
			str:        "foo",
			ignoreCase: true,
			want: &matcher.StringMatcher{
				IgnoreCase: true,
				MatchPattern: &matcher.StringMatcher_Exact{
					Exact: "foo",
				},
			},
		},
		{
			name:       "exact2",
			str:        "bar",
			ignoreCase: false,
			want: &matcher.StringMatcher{
				IgnoreCase: false,
				MatchPattern: &matcher.StringMatcher_Exact{
					Exact: "bar",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := StringMatcherExact(tc.str, tc.ignoreCase)
			if !cmp.Equal(got, tc.want, protocmp.Transform()) {
				t.Errorf("want %v but got %v", tc.want, got)
			}
		})
	}
}
