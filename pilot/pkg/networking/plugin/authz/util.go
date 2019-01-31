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
	"net"
	"strconv"
	"strings"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/gogo/protobuf/types"
)

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

// convertToCidr converts a CIDR or a single IP string to a corresponding CidrRange. For a single IP
// string the converted CidrRange prefix is either 32 (for ipv4) or 128 (for ipv6).
func convertToCidr(v string) (*core.CidrRange, error) {
	var address string
	var prefixLen int

	if strings.Contains(v, "/") {
		if ip, ipnet, err := net.ParseCIDR(v); err == nil {
			address = ip.String()
			prefixLen, _ = ipnet.Mask.Size()
		} else {
			return nil, fmt.Errorf("invalid cidr range: %v", err)
		}
	} else {
		if ip := net.ParseIP(v); ip != nil {
			address = ip.String()
			if strings.Contains(v, ".") {
				// Set the prefixLen to 32 for ipv4 address.
				prefixLen = 32
			} else if strings.Contains(v, ":") {
				// Set the prefixLen to 128 for ipv6 address.
				prefixLen = 128
			} else {
				return nil, fmt.Errorf("invalid ip address: %s", v)
			}
		} else {
			return nil, fmt.Errorf("invalid ip address: %s", v)
		}
	}

	return &core.CidrRange{
		AddressPrefix: address,
		PrefixLen:     &types.UInt32Value{Value: uint32(prefixLen)},
	}, nil
}

// convertToPort converts a port string to a uint32.
func convertToPort(v string) (uint32, error) {
	p, err := strconv.ParseUint(v, 10, 32)
	if err != nil || p > 65535 {
		return 0, fmt.Errorf("invalid port %s: %v", v, err)
	}
	return uint32(p), nil
}

// convertToHeaderMatcher converts a key, value string pair to a corresponding HeaderMatcher.
func convertToHeaderMatcher(k, v string) *route.HeaderMatcher {
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

// Check if the key is of format a[b].
func isKeyBinary(key string) bool {
	open := strings.Index(key, "[")
	return strings.HasSuffix(key, "]") && open > 0 && open < len(key)-2
}

func extractNameInBrackets(s string) (string, error) {
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return "", fmt.Errorf("expecting format [<NAME>], but found %s", s)
	}
	return strings.TrimPrefix(strings.TrimSuffix(s, "]"), "["), nil
}

// extractActualServiceAccount extracts the actual service account from the Istio service account if
// found any, otherwise it returns the Istio service account itself without any change.
// Istio service account has the format: "spiffe://<domain>/ns/<namespace>/sa/<service-account>"
func extractActualServiceAccount(istioServiceAccount string) string {
	actualSA := istioServiceAccount
	beginName, optionalEndName := "sa/", "/"
	beginIndex := strings.Index(actualSA, beginName)
	if beginIndex != -1 {
		beginIndex += len(beginName)
		length := strings.Index(actualSA[beginIndex:], optionalEndName)
		if length == -1 {
			actualSA = actualSA[beginIndex:]
		} else {
			actualSA = actualSA[beginIndex : beginIndex+length]
		}
	}
	return actualSA
}
