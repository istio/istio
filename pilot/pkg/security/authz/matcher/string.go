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

	matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher"
)

// StringMatcher creates a string matcher for v.
func StringMatcher(v string) *matcherpb.StringMatcher {
	return StringMatcherWithPrefix(v, "")
}

// StringMatcherRegex creates a regex string matcher for regex.
func StringMatcherRegex(regex string) *matcherpb.StringMatcher {
	return &matcherpb.StringMatcher{
		MatchPattern: &matcherpb.StringMatcher_SafeRegex{
			SafeRegex: &matcherpb.RegexMatcher{
				EngineType: &matcherpb.RegexMatcher_GoogleRe2{
					GoogleRe2: &matcherpb.RegexMatcher_GoogleRE2{},
				},
				Regex: regex,
			},
		},
	}
}

// StringMatcherWithPrefix creates a string matcher for v with the extra prefix inserted to the
// created string matcher, note the prefix is ignored if v is wildcard ("*").
// The wildcard "*" will be generated as ".+" instead of ".*".
func StringMatcherWithPrefix(v, prefix string) *matcherpb.StringMatcher {
	switch {
	// Check if v is "*" first to make sure we won't generate an empty prefix/suffix StringMatcher,
	// the Envoy StringMatcher doesn't allow empty prefix/suffix.
	case v == "*":
		return StringMatcherRegex(".+")
	case strings.HasPrefix(v, "*"):
		if prefix == "" {
			return &matcherpb.StringMatcher{
				MatchPattern: &matcherpb.StringMatcher_Suffix{
					Suffix: strings.TrimPrefix(v, "*"),
				},
			}
		}
		return StringMatcherRegex(prefix + ".*" + strings.TrimPrefix(v, "*"))
	case strings.HasSuffix(v, "*"):
		return &matcherpb.StringMatcher{
			MatchPattern: &matcherpb.StringMatcher_Prefix{
				Prefix: prefix + strings.TrimSuffix(v, "*"),
			},
		}
	default:
		return &matcherpb.StringMatcher{
			MatchPattern: &matcherpb.StringMatcher_Exact{
				Exact: prefix + v,
			},
		}
	}
}
