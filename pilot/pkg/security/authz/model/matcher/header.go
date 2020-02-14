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

	route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	envoy_matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher"
)

// HeaderMatcher converts a key, value string pair to a corresponding HeaderMatcher.
func HeaderMatcher(k, v string) *route.HeaderMatcher {
	// We must check "*" first to make sure we'll generate a non empty value in the prefix/suffix case.
	// Empty prefix/suffix value is invalid in HeaderMatcher.
	if v == "*" {
		return &route.HeaderMatcher{
			Name: k,
			HeaderMatchSpecifier: &route.HeaderMatcher_PresentMatch{
				PresentMatch: true,
			},
		}
	} else if strings.HasPrefix(v, "*") {
		return &route.HeaderMatcher{
			Name: k,
			HeaderMatchSpecifier: &route.HeaderMatcher_SuffixMatch{
				SuffixMatch: v[1:],
			},
		}
	} else if strings.HasSuffix(v, "*") {
		return &route.HeaderMatcher{
			Name: k,
			HeaderMatchSpecifier: &route.HeaderMatcher_PrefixMatch{
				PrefixMatch: v[:len(v)-1],
			},
		}
	}
	return &route.HeaderMatcher{
		Name: k,
		HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{
			ExactMatch: v,
		},
	}
}

// PathMatcher creates a path matcher for a path.
func PathMatcher(path string) *envoy_matcher.PathMatcher {
	return &envoy_matcher.PathMatcher{
		Rule: &envoy_matcher.PathMatcher_Path{
			Path: StringMatcher(path, true),
		},
	}
}
