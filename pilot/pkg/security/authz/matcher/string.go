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
	"strings"

	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
)

// StringMatcher creates a string matcher for v.
func StringMatcher(v string) *matcher.StringMatcher {
	return StringMatcherWithPrefix(v, "")
}

// StringOrMatcher creates an OR string matcher for a list of values.
func StringOrMatcher(vs []string) *matcher.ValueMatcher {
	matchers := []*matcher.ValueMatcher{}
	for _, v := range vs {
		matchers = append(matchers, &matcher.ValueMatcher{
			MatchPattern: &matcher.ValueMatcher_StringMatch{
				StringMatch: StringMatcher(v),
			},
		})
	}
	return OrMatcher(matchers)
}

// OrMatcher creates an OR matcher for a list of matchers.
func OrMatcher(matchers []*matcher.ValueMatcher) *matcher.ValueMatcher {
	if len(matchers) == 1 {
		return matchers[0]
	}
	return &matcher.ValueMatcher{
		MatchPattern: &matcher.ValueMatcher_OrMatch{
			OrMatch: &matcher.OrMatcher{
				ValueMatchers: matchers,
			},
		},
	}
}

// StringMatcherRegex creates a regex string matcher for regex.
func StringMatcherRegex(regex string) *matcher.StringMatcher {
	return &matcher.StringMatcher{
		MatchPattern: &matcher.StringMatcher_SafeRegex{
			SafeRegex: &matcher.RegexMatcher{
				Regex: regex,
			},
		},
	}
}

// StringMatcherPrefix create a string matcher for prefix matching.
func StringMatcherPrefix(prefix string, ignoreCase bool) *matcher.StringMatcher {
	return &matcher.StringMatcher{
		IgnoreCase: ignoreCase,
		MatchPattern: &matcher.StringMatcher_Prefix{
			Prefix: prefix,
		},
	}
}

// StringMatcherSuffix create a string matcher for suffix matching.
func StringMatcherSuffix(suffix string, ignoreCase bool) *matcher.StringMatcher {
	return &matcher.StringMatcher{
		IgnoreCase: ignoreCase,
		MatchPattern: &matcher.StringMatcher_Suffix{
			Suffix: suffix,
		},
	}
}

// StringMatcherExact create a string matcher for exact matching.
func StringMatcherExact(exact string, ignoreCase bool) *matcher.StringMatcher {
	return &matcher.StringMatcher{
		IgnoreCase: ignoreCase,
		MatchPattern: &matcher.StringMatcher_Exact{
			Exact: exact,
		},
	}
}

// StringMatcherWithPrefix creates a string matcher for v with the extra prefix inserted to the
// created string matcher, note the prefix is ignored if v is wildcard ("*").
// The wildcard "*" will be generated as ".+" instead of ".*".
func StringMatcherWithPrefix(v, prefix string) *matcher.StringMatcher {
	switch {
	// Check if v is "*" first to make sure we won't generate an empty prefix/suffix StringMatcher,
	// the Envoy StringMatcher doesn't allow empty prefix/suffix.
	case v == "*":
		return StringMatcherRegex(".+")
	case strings.HasPrefix(v, "*"):
		if prefix == "" {
			return StringMatcherSuffix(strings.TrimPrefix(v, "*"), false)
		}
		return StringMatcherRegex(prefix + ".*" + strings.TrimPrefix(v, "*"))
	case strings.HasSuffix(v, "*"):
		return StringMatcherPrefix(prefix+strings.TrimSuffix(v, "*"), false)
	default:
		return StringMatcherExact(prefix+v, false)
	}
}
