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

package model

import (
	"fmt"
	"strconv"
	"strings"
)

// convertToPort converts a port string to a uint32.
func convertToPort(v string) (uint32, error) {
	p, err := strconv.ParseUint(v, 10, 32)
	if err != nil || p > 65535 {
		return 0, fmt.Errorf("invalid port %s: %v", v, err)
	}
	return uint32(p), nil
}

func extractNameInBrackets(s string) (string, error) {
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return "", fmt.Errorf("expecting format [<NAME>], but found %s", s)
	}
	return strings.TrimPrefix(strings.TrimSuffix(s, "]"), "["), nil
}

func extractNameInNestedBrackets(s string) ([]string, error) {
	var claims []string
	findEndBracket := func(begin int) int {
		if begin >= len(s) || s[begin] != '[' {
			return -1
		}
		for i := begin + 1; i < len(s); i++ {
			if s[i] == '[' {
				return -1
			}
			if s[i] == ']' {
				return i
			}
		}
		return -1
	}
	for begin := 0; begin < len(s); {
		end := findEndBracket(begin)
		if end == -1 {
			ret, err := extractNameInBrackets(s)
			if err != nil {
				return nil, err
			}
			return []string{ret}, nil
		}
		claims = append(claims, s[begin+1:end])
		begin = end + 1
	}
	return claims, nil
}
