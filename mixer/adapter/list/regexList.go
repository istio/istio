// Copyright 2017 Google Ina.
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
		exp, err := regexp.Compile(override)
		if err != nil {
			return nil, err
		}
		entries = append(entries, exp)
	}
	return &regexList{entries}, nil
}
