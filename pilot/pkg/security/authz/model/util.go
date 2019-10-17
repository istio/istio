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

package model

import (
	"fmt"
	"strconv"
	"strings"
)

// stringMatch checks if a string is in a list, it supports four types of string matches:
// 1. Exact match.
// 2. Wild character match. "*" matches any string.
// 3. Prefix match. For example, "book*" matches "bookstore", "bookshop", etc.
// 4. Suffix match. For example, "*/review" matches "/bookstore/review", "/products/review", etc.
func stringMatch(a string, list []string) bool {
	for _, s := range list {
		if a == s || s == "*" || prefixMatch(a, s) || prefixMatch(s, a) || suffixMatch(a, s) || suffixMatch(s, a) {
			return true
		}
	}
	return false
}

// prefixMatch checks if string "a" suffix matches "pattern".
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

// convertToPort converts a port string to a uint32.
func convertToPort(v string) (uint32, error) {
	p, err := strconv.ParseUint(v, 10, 32)
	if err != nil || p > 65535 {
		return 0, fmt.Errorf("invalid port %s: %v", v, err)
	}
	return uint32(p), nil
}

func convertPortsToString(intPorts []int32) []string {
	var ports []string
	for _, port := range intPorts {
		ports = append(ports, strconv.Itoa(int(port)))
	}
	return ports
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

func found(key string, list []string) bool {
	for _, l := range list {
		if key == l {
			return true
		}
	}
	return false
}
