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

package path

import (
	"path/filepath"
	"strings"
)

var (
	// pathSeparator is the separator between path elements.
	pathSeparator = "/"
	// escapedPathSeparator is what to use when the path shouldn't separate
	escapedPathSeparator = "\\" + pathSeparator
)

// Path is a path in slice form.
type Path []string

// FromString converts a string path of form a.b.c to a string slice representation.
func FromString(path string) Path {
	path = filepath.Clean(path)
	path = strings.TrimPrefix(path, pathSeparator)
	path = strings.TrimSuffix(path, pathSeparator)
	pv := splitEscaped(path, []rune(pathSeparator)[0])
	var r []string
	for _, str := range pv {
		if str != "" {
			str = strings.ReplaceAll(str, escapedPathSeparator, pathSeparator)
			// Is str of the form node[expr], convert to "node", "[expr]"?
			nBracket := strings.IndexRune(str, '[')
			if nBracket > 0 {
				r = append(r, str[:nBracket], str[nBracket:])
			} else {
				// str is "[expr]" or "node"
				r = append(r, str)
			}
		}
	}
	return r
}

// String converts a string slice path representation of form ["a", "b", "c"] to a string representation like "a.b.c".
func (p Path) String() string {
	return strings.Join(p, pathSeparator)
}

// splitEscaped splits a string using the rune r as a separator. It does not split on r if it's prefixed by \.
func splitEscaped(s string, r rune) []string {
	var prev rune
	if len(s) == 0 {
		return []string{}
	}
	prevIdx := 0
	var out []string
	for i, c := range s {
		if c == r && (i == 0 || (i > 0 && prev != '\\')) {
			out = append(out, s[prevIdx:i])
			prevIdx = i + 1
		}
		prev = c
	}
	out = append(out, s[prevIdx:])
	return out
}
