// Copyright 2018 The Operator-SDK Authors
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

// Based on https://github.com/iancoleman/strcase

package paramconv

import (
	"regexp"
	"strings"
)

var (
	numberSequence    = regexp.MustCompile(`([a-zA-Z])(\d+)([a-zA-Z]?)`)
	numberReplacement = []byte(`$1 $2 $3`)
	wordMapping       = map[string]string{
		"http": "HTTP",
		"url":  "URL",
		"ip":   "IP",
	}
)

func addWordBoundariesToNumbers(s string) string {
	b := []byte(s)
	b = numberSequence.ReplaceAll(b, numberReplacement)
	return string(b)
}

func translateWord(word string, initCase bool) string {
	if val, ok := wordMapping[word]; ok {
		return val
	}
	if initCase {
		return strings.Title(word)
	}
	return word
}

// Converts a string to CamelCase
func ToCamel(s string) string {
	s = addWordBoundariesToNumbers(s)
	s = strings.Trim(s, " ")
	n := ""
	bits := []string{}
	for _, v := range s {
		if v == '_' || v == ' ' || v == '-' {
			bits = append(bits, n)
			n = ""
		} else {
			n += string(v)
		}
	}
	bits = append(bits, n)

	ret := ""
	for i, substr := range bits {
		ret += translateWord(substr, i != 0)
	}
	return ret
}

// Converts a string to snake_case
func ToSnake(s string) string {
	s = addWordBoundariesToNumbers(s)
	s = strings.Trim(s, " ")
	var prefix string
	char1 := []rune(s)[0]
	if char1 >= 'A' && char1 <= 'Z' {
		prefix = "_"
	} else {
		prefix = ""
	}
	bits := []string{}
	n := ""
	real_i := -1

	for i, v := range s {
		real_i += 1
		// treat acronyms as words, eg for JSONData -> JSON is a whole word
		nextCaseIsChanged := false
		if i+1 < len(s) {
			next := s[i+1]
			if (v >= 'A' && v <= 'Z' && next >= 'a' && next <= 'z') || (v >= 'a' && v <= 'z' && next >= 'A' && next <= 'Z') {
				nextCaseIsChanged = true
			}
		}

		if real_i > 0 && n[len(n)-1] != '_' && nextCaseIsChanged {
			// add underscore if next letter case type is changed
			if v >= 'A' && v <= 'Z' {
				bits = append(bits, strings.ToLower(n))
				n = string(v)
				real_i = 0
			} else if v >= 'a' && v <= 'z' {
				bits = append(bits, strings.ToLower(n+string(v)))
				n = ""
				real_i = -1
			}
		} else if v == ' ' || v == '_' || v == '-' {
			// replace spaces/underscores with delimiters
			bits = append(bits, strings.ToLower(n))
			n = ""
			real_i = -1
		} else {
			n = n + string(v)
		}
	}
	bits = append(bits, strings.ToLower(n))
	joined := strings.Join(bits, "_")
	if _, ok := wordMapping[bits[0]]; !ok {
		return prefix + joined
	}
	return joined
}

func convertParameter(fn func(string) string, v interface{}) interface{} {
	switch v := v.(type) {
	case map[string]interface{}:
		ret := map[string]interface{}{}
		for key, val := range v {
			ret[fn(key)] = convertParameter(fn, val)
		}
		return ret
	case []interface{}:
		return convertArray(fn, v)
	default:
		return v
	}
}

func convertArray(fn func(string) string, in []interface{}) []interface{} {
	res := make([]interface{}, len(in))
	for i, v := range in {
		res[i] = convertParameter(fn, v)
	}
	return res
}

func convertMapKeys(fn func(string) string, in map[string]interface{}) map[string]interface{} {
	converted := map[string]interface{}{}
	for key, val := range in {
		converted[fn(key)] = convertParameter(fn, val)
	}
	return converted
}

func MapToSnake(in map[string]interface{}) map[string]interface{} {
	return convertMapKeys(ToSnake, in)
}

func MapToCamel(in map[string]interface{}) map[string]interface{} {
	return convertMapKeys(ToCamel, in)
}
