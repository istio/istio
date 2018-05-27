// Copyright 2018 Istio Authors
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

package authz

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/envoyproxy/go-control-plane/envoy/type"
	"github.com/gogo/protobuf/types"
)

// convertToStringMatch converts a string to a StringMatch, it supports four types of conversion:
// 1. Wild caracter match. i.e. "*" is converted to a regular expression match of "*"
// 2. Suffix match, e.g. "*abc" is converted to a suffix match of "abc"
// 3. Prefix match, e.g. "abc* " is converted to a prefix match of "abc"
// 4. Exact match, e.g. "abc" is converted to a simple exact match of "abc"
func convertToStringMatch(s string) *envoy_type.StringMatch {
	s = strings.TrimSpace(s)
	switch {
	case s == "*":
		return &envoy_type.StringMatch{MatchPattern: &envoy_type.StringMatch_Regex{Regex: "*"}}
	case strings.HasPrefix(s, "*"):
		return &envoy_type.StringMatch{MatchPattern: &envoy_type.StringMatch_Suffix{Suffix: strings.TrimPrefix(s, "*")}}
	case strings.HasSuffix(s, "*"):
		return &envoy_type.StringMatch{MatchPattern: &envoy_type.StringMatch_Prefix{Prefix: strings.TrimSuffix(s, "*")}}
	default:
		return &envoy_type.StringMatch{MatchPattern: &envoy_type.StringMatch_Simple{Simple: s}}
	}
}

// stringMatch checks if a string is in a list, it supports four types of string matches:
// 1. Exact match.
// 2. Wild character match. "*" matches any string.
// 3. Prefix match. For example, "book*" matches "bookstore", "bookshop", etc.
// 4. Suffix match. For example, "*/review" matches "/bookstore/review", "/products/review", etc.
func stringMatch(a string, list []string) bool {
	for _, s := range list {
		if a == s || s == "*" || prefixMatch(a, s) || suffixMatch(a, s) {
			return true
		}
	}
	return false
}

// prefixMatch checks if string "a" prefix matches "pattern".
func prefixMatch(a string, pattern string) bool {
	if !strings.HasSuffix(pattern, "*") {
		return false
	}
	pattern = strings.TrimSuffix(pattern, "*")
	return strings.HasPrefix(a, pattern)
}

// suffixMatch checks if string "a" prefix matches "pattern".
func suffixMatch(a string, pattern string) bool {
	if !strings.HasPrefix(pattern, "*") {
		return false
	}
	pattern = strings.TrimPrefix(pattern, "*")
	return strings.HasSuffix(a, pattern)
}

func convertToCidr(v string) (*core.CidrRange, error) {
	splits := strings.Split(v, "/")
	if len(splits) != 2 {
		return nil, fmt.Errorf("invalid cidr range: %s", v)
	}
	prefixLen, err := strconv.ParseUint(splits[1], 10, 32)
	if err != nil || prefixLen < 0 {
		return nil, fmt.Errorf("invalid prefix length in cidr range: %s: %v", splits[1], err)
	}
	return &core.CidrRange{
		AddressPrefix: splits[0],
		PrefixLen:     &types.UInt32Value{Value: uint32(prefixLen)},
	}, nil
}

func convertToPort(v string) (uint32, error) {
	p, err := strconv.ParseUint(v, 10, 32)
	if err != nil || p < 0 || p > 65535 {
		return 0, fmt.Errorf("invalid port %s: %v", v, err)
	}
	return uint32(p), nil
}

func convertToHeaderMatcher(k, v string) *route.HeaderMatcher {
	//TODO(yangminzhu): Update the HeaderMatcher to support prefix and suffix match.
	if strings.Contains(v, "*") {
		return &route.HeaderMatcher{
			Name: k,
			HeaderMatchSpecifier: &route.HeaderMatcher_RegexMatch{
				RegexMatch: "^" + strings.Replace(v, "*", ".*", -1) + "$",
			},
		}
	} else {
		return &route.HeaderMatcher{
			Name: k,
			HeaderMatchSpecifier: &route.HeaderMatcher_ExactMatch{
				ExactMatch: v,
			},
		}
	}
}
