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

package list

import (
	"regexp"
	"strings"
)

type regexList struct {
	regexpList []*regexp.Regexp
}

func (l *regexList) checkList(symbol string) (bool, error) {
	for _, exp := range l.regexpList {
		if exp.MatchString(symbol) {
			return true, nil
		}
	}
	return false, nil
}

func (l *regexList) numEntries() int {
	return len(l.regexpList)
}

// parseRegexList parses regexp list from buf and overrides. buf is assumed to be '\n' separated regular expressions.
// Each entry in overrides is a regular expression as well. regexList accepts RE2 regex syntax.
// See https://github.com/google/re2/wiki/Syntax
// Example:
// buf:  "a+.*\nabc" expands to two regex, "a+.*" and "abc"
func parseRegexList(buf []byte, overrides []string) (*regexList, error) {
	lines := strings.Split(string(buf), "\n")
	entries := make([]*regexp.Regexp, 0, len(lines)+len(overrides))
	for _, line := range lines {
		if line != "" {
			exp, err := regexp.Compile(line)
			if err != nil {
				return nil, err
			}
			entries = append(entries, exp)
		}
	}

	for _, override := range overrides {
		// cannot fail, override syntax was checked in the Validate method
		exp, _ := regexp.Compile(override)
		entries = append(entries, exp)
	}
	return &regexList{entries}, nil
}
