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

package resource

import (
	"regexp"
	"strconv"
	"strings"
)

// testFilter is a regex matcher on a test. It is split by / following go subtest format
type testFilter []*regexp.Regexp

type Matcher struct {
	filters []testFilter
}

// NewMatcher reimplements the logic of Go's -test.run. The code is mostly directly copied from Go's source.
func NewMatcher(regexs []string) (*Matcher, error) {
	filters := []testFilter{}
	for _, regex := range regexs {
		filter := splitRegexp(regex)
		for i, s := range filter {
			filter[i] = rewrite(s)
		}
		rxs := []*regexp.Regexp{}
		for _, f := range filter {
			r, err := regexp.Compile(f)
			if err != nil {
				return nil, err
			}
			rxs = append(rxs, r)
		}
		filters = append(filters, rxs)
	}
	return &Matcher{filters: filters}, nil
}

func (m *Matcher) MatchTest(testName string) bool {
	for _, f := range m.filters {
		if matchSingle(f, testName) {
			return true
		}
	}
	return false
}

func matchSingle(filter testFilter, testName string) bool {
	if len(filter) == 0 {
		// No regex defined, we default to NOT matching. This ensures our default skips nothing
		return false
	}
	elem := strings.Split(testName, "/")
	if len(filter) > len(elem) {
		return false
	}
	for i, s := range elem {
		if i >= len(filter) {
			break
		}
		if !filter[i].MatchString(s) {
			return false
		}
	}
	return true
}

// From go/src/testing/match.go
// nolint
func splitRegexp(s string) []string {
	a := make([]string, 0, strings.Count(s, "/"))
	cs := 0
	cp := 0
	for i := 0; i < len(s); {
		switch s[i] {
		case '[':
			cs++
		case ']':
			if cs--; cs < 0 { // An unmatched ']' is legal.
				cs = 0
			}
		case '(':
			if cs == 0 {
				cp++
			}
		case ')':
			if cs == 0 {
				cp--
			}
		case '\\':
			i++
		case '/':
			if cs == 0 && cp == 0 {
				a = append(a, s[:i])
				s = s[i+1:]
				i = 0
				continue
			}
		}
		i++
	}
	return append(a, s)
}

// rewrite rewrites a subname to having only printable characters and no white
// space.
// From go/src/testing/match.go
func rewrite(s string) string {
	b := []byte{}
	for _, r := range s {
		switch {
		case isSpace(r):
			b = append(b, '_')
		case !strconv.IsPrint(r):
			s := strconv.QuoteRune(r)
			b = append(b, s[1:len(s)-1]...)
		default:
			b = append(b, string(r)...)
		}
	}
	return string(b)
}

// From go/src/testing/match.go
func isSpace(r rune) bool {
	if r < 0x2000 {
		switch r {
		// Note: not the same as Unicode Z class.
		case '\t', '\n', '\v', '\f', '\r', ' ', 0x85, 0xA0, 0x1680:
			return true
		}
	} else {
		if r <= 0x200a {
			return true
		}
		switch r {
		case 0x2028, 0x2029, 0x202f, 0x205f, 0x3000:
			return true
		}
	}
	return false
}
