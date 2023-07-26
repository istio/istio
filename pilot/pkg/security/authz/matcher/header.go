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
	"fmt"
	"regexp"
	"strings"

	routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
)

// HeaderMatcher converts a key, value string pair to a corresponding HeaderMatcher.
func HeaderMatcher(k, v string) *routepb.HeaderMatcher {
	// We must check "*" first to make sure we'll generate a non empty value in the prefix/suffix case.
	// Empty prefix/suffix value is invalid in HeaderMatcher.
	if v == "*" {
		return &routepb.HeaderMatcher{
			Name: k,
			HeaderMatchSpecifier: &routepb.HeaderMatcher_PresentMatch{
				PresentMatch: true,
			},
		}
	} else if strings.HasPrefix(v, "*") {
		return &routepb.HeaderMatcher{
			Name: k,
			HeaderMatchSpecifier: &routepb.HeaderMatcher_StringMatch{
				StringMatch: StringMatcherSuffix(v[1:], false),
			},
		}
	} else if strings.HasSuffix(v, "*") {
		return &routepb.HeaderMatcher{
			Name: k,
			HeaderMatchSpecifier: &routepb.HeaderMatcher_StringMatch{
				StringMatch: StringMatcherPrefix(v[:len(v)-1], false),
			},
		}
	}
	return &routepb.HeaderMatcher{
		Name: k,
		HeaderMatchSpecifier: &routepb.HeaderMatcher_StringMatch{
			StringMatch: StringMatcherExact(v, false),
		},
	}
}

// HeaderMatcherWithRegex converts a key, value string pair to a corresponding
// HeaderMatcher to support matching the value presented in the inline header with
// multiple values concatenated by commas, as per RFC7230.
func HeaderMatcherWithRegex(k, v string) *routepb.HeaderMatcher {
	// We must check "*" first to distinguish present match from the other
	// cases.
	var regex string
	if v == "*" {
		return &routepb.HeaderMatcher{
			Name: k,
			HeaderMatchSpecifier: &routepb.HeaderMatcher_PresentMatch{
				PresentMatch: true,
			},
		}
	} else if strings.HasPrefix(v, "*") {
		regex = `.*` + regexp.QuoteMeta(v[1:])
	} else if strings.HasSuffix(v, "*") {
		regex = regexp.QuoteMeta(v[:len(v)-1]) + `.*`
	} else {
		regex = regexp.QuoteMeta(v)
	}
	return &routepb.HeaderMatcher{
		Name: k,
		HeaderMatchSpecifier: &routepb.HeaderMatcher_SafeRegexMatch{
			SafeRegexMatch: &matcher.RegexMatcher{
				Regex: fmt.Sprintf(`^%s$|^%s,.*|.*,%s,.*|.*,%s$`, regex, regex, regex, regex),
			},
		},
	}
}

// HostMatcher creates a host matcher for a host.
func HostMatcher(k, v string) *routepb.HeaderMatcher {
	// We must check "*" first to make sure we'll generate a non empty value in the prefix/suffix case.
	// Empty prefix/suffix value is invalid in HeaderMatcher.
	if v == "*" {
		return &routepb.HeaderMatcher{
			Name: k,
			HeaderMatchSpecifier: &routepb.HeaderMatcher_PresentMatch{
				PresentMatch: true,
			},
		}
	} else if strings.HasPrefix(v, "*") {
		return &routepb.HeaderMatcher{
			Name: k,
			HeaderMatchSpecifier: &routepb.HeaderMatcher_StringMatch{
				StringMatch: StringMatcherSuffix(v[1:], true),
			},
		}
	} else if strings.HasSuffix(v, "*") {
		return &routepb.HeaderMatcher{
			Name: k,
			HeaderMatchSpecifier: &routepb.HeaderMatcher_StringMatch{
				StringMatch: StringMatcherPrefix(v[:len(v)-1], true),
			},
		}
	}
	return &routepb.HeaderMatcher{
		Name: k,
		HeaderMatchSpecifier: &routepb.HeaderMatcher_StringMatch{
			StringMatch: StringMatcherExact(v, true),
		},
	}
}

// PathMatcher creates a path matcher for a path.
func PathMatcher(path string) *matcher.PathMatcher {
	return &matcher.PathMatcher{
		Rule: &matcher.PathMatcher_Path{
			Path: StringMatcher(path),
		},
	}
}
