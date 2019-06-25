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

package util

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	kvSeparatorRune = ':'

	// PathSeparator is the separator between path elements.
	PathSeparator = "."
	// KVSeparator is the separator between the key and value in a key/value path element,
	KVSeparator = string(kvSeparatorRune)
)

var (
	// ValidKeyRegex is a regex for a valid path key element.
	ValidKeyRegex = regexp.MustCompile("^[a-zA-Z0-9_-]*$")
)

// Path is a path in slice form.
type Path []string

// PathFromString converts a string path of form a.b.c to a string slice representation.
func PathFromString(path string) []string {
	path = filepath.Clean(path)
	path = strings.TrimPrefix(path, PathSeparator)
	path = strings.TrimSuffix(path, PathSeparator)
	pv := strings.Split(path, PathSeparator)
	var r []string
	for _, str := range pv {
		if str != "" {
			r = append(r, str)
		}
	}
	return r
}

// String converts a string slice path representation of form ["a", "b", "c"] to a string representation like "a.b.c".
func (p Path) String() string {
	return strings.Join(p, PathSeparator)
}

// IsValidPathElement reports whether pe is a valid path element.
func IsValidPathElement(pe string) bool {
	return ValidKeyRegex.MatchString(pe)
}

// IsKVPathElement report whether pe is a key/value path element.
func IsKVPathElement(pe string) bool {
	pe, ok := RemoveBrackets(pe)
	if !ok {
		return false
	}

	kv := splitEscaped(pe, kvSeparatorRune)
	if len(kv) != 2 || len(kv[0]) == 0 || len(kv[1]) == 0 {
		return false
	}
	return IsValidPathElement(kv[0])
}

// IsVPathElement report whether pe is a value path element.
func IsVPathElement(pe string) bool {
	pe, ok := RemoveBrackets(pe)
	if !ok {
		return false
	}

	return len(pe) > 0
}

// Path KVreturns the key and value string parts of the entire key/value path element.
// It returns an error if pe is not a key/value path element.
func PathKV(pe string) (k, v string, err error) {
	if !IsKVPathElement(pe) {
		return "", "", fmt.Errorf("%s is not a valid key:value path element", pe)
	}
	pe, _ = RemoveBrackets(pe)
	kv := splitEscaped(pe, kvSeparatorRune)
	return kv[0], kv[1], nil
}

// PathV returns the value string part of the entire value path element.
// It returns an error if pe is not a value path element.
func PathV(pe string) (string, error) {
	if !IsVPathElement(pe) {
		return "", fmt.Errorf("%s is not a valid value path element", pe)
	}
	v, _ := RemoveBrackets(pe)
	return v, nil
}

// RemoveBrackets removes the [] around pe and returns the resulting string. It returns false if pe is not surrounded
// by [].
func RemoveBrackets(pe string) (string, bool) {
	if !strings.HasPrefix(pe, "[") || !strings.HasSuffix(pe, "]") {
		return "", false
	}
	return pe[1 : len(pe)-1], true
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
