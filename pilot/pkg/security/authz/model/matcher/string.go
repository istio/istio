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
	"strings"

	envoy_matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher"
)

// StringMatcher creates a string matcher for v.
func StringMatcher(v string, treatWildcardAsRequired bool) *envoy_matcher.StringMatcher {
	return StringMatcherWithPrefix(v, "", treatWildcardAsRequired)
}

// StringMatcherRegex creates a regex string matcher for regex.
func StringMatcherRegex(regex string) *envoy_matcher.StringMatcher {
	return &envoy_matcher.StringMatcher{
		MatchPattern: &envoy_matcher.StringMatcher_SafeRegex{
			SafeRegex: &envoy_matcher.RegexMatcher{
				EngineType: &envoy_matcher.RegexMatcher_GoogleRe2{
					GoogleRe2: &envoy_matcher.RegexMatcher_GoogleRE2{},
				},
				Regex: regex,
			},
		},
	}
}

// StringMatcherWithPrefix creates a string matcher for v with the extra prefix inserted to the
// created string matcher, note the prefix is ignored if v is wildcard ("*").
// If treatWildcardAsRequired is true, the wildcard "*" will be generated as ".+" instead of ".*".
func StringMatcherWithPrefix(v, prefix string, treatWildcardAsRequired bool) *envoy_matcher.StringMatcher {
	switch {
	// Check if v is "*" first to make sure we won't generate an empty prefix/suffix StringMatcher,
	// the Envoy StringMatcher doesn't allow empty prefix/suffix.
	case v == "*":
		if treatWildcardAsRequired {
			return StringMatcherRegex(".+")
		}
		return StringMatcherRegex(".*")
	case strings.HasPrefix(v, "*"):
		return &envoy_matcher.StringMatcher{
			MatchPattern: &envoy_matcher.StringMatcher_Suffix{
				Suffix: prefix + strings.TrimPrefix(v, "*"),
			},
		}
	case strings.HasSuffix(v, "*"):
		return &envoy_matcher.StringMatcher{
			MatchPattern: &envoy_matcher.StringMatcher_Prefix{
				Prefix: prefix + strings.TrimSuffix(v, "*"),
			},
		}
	default:
		return &envoy_matcher.StringMatcher{
			MatchPattern: &envoy_matcher.StringMatcher_Exact{
				Exact: prefix + v,
			},
		}
	}
}
