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

package strcase

import (
	"bytes"
	"strings"
)

// CamelCase converts the string into camel case string
func CamelCase(s string) string {
	if s == "" {
		return ""
	}
	t := make([]byte, 0, 32)
	i := 0
	if isWordSeparator(s[0]) {
		// Need a capital letter; drop the '_'.
		t = append(t, 'X')
		i++
	}
	// Invariant: if the next letter is lower case, it must be converted
	// to upper case.
	// That is, we process a word at a time, where words are marked by _, - or
	// upper case letter. Digits are treated as words.
	for ; i < len(s); i++ {
		c := s[i]
		if isWordSeparator(c) {
			// Skip the separate and capitalize the next letter.
			continue
		}
		if isASCIIDigit(c) {
			t = append(t, c)
			continue
		}
		// Assume we have a letter now - if not, it's a bogus identifier.
		// The next word is a sequence of characters that must start upper case.
		if isASCIILower(c) {
			c ^= ' ' // Make it a capital letter.
		}
		t = append(t, c) // Guaranteed not lower case.
		// Accept lower case sequence that follows.
		for i+1 < len(s) && isASCIILower(s[i+1]) {
			i++
			t = append(t, s[i])
		}
	}
	return string(t)
}

// CamelCaseWithSeparator splits the given string by the separator, converts the parts to CamelCase and then re-joins them.
func CamelCaseWithSeparator(n string, sep string) string {
	p := strings.Split(n, sep)
	for i := 0; i < len(p); i++ {
		p[i] = CamelCase(p[i])
	}
	return strings.Join(p, "")
}

// CamelCaseToKebabCase converts "MyName" to "my-name"
func CamelCaseToKebabCase(s string) string {
	switch s {
	case "HTTPAPISpec":
		return "http-api-spec"
	case "HTTPRoute":
		return "http-route"
	case "HTTPAPISpecBinding":
		return "http-api-spec-binding"
	default:
		var out bytes.Buffer
		for i := range s {
			if 'A' <= s[i] && s[i] <= 'Z' {
				if i > 0 {
					out.WriteByte('-')
				}
				out.WriteByte(s[i] - 'A' + 'a')
			} else {
				out.WriteByte(s[i])
			}
		}
		return out.String()
	}
}

func isWordSeparator(c byte) bool {
	return c == '_' || c == '-'
}

// Is c an ASCII lower-case letter?
func isASCIILower(c byte) bool {
	return 'a' <= c && c <= 'z'
}

// Is c an ASCII digit?
func isASCIIDigit(c byte) bool {
	return '0' <= c && c <= '9'
}
