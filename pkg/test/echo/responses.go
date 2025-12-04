//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package echo

import (
	"fmt"
	"strings"
)

// Responses is an ordered list of parsed response objects.
type Responses []Response

func (r Responses) IsEmpty() bool {
	return len(r) == 0
}

// Len returns the length of the parsed responses.
func (r Responses) Len() int {
	return len(r)
}

// Count occurrences of the given text within the bodies of all responses.
func (r Responses) Count(text string) int {
	count := 0
	for _, c := range r {
		count += c.Count(text)
	}
	return count
}

// Match returns a subset of Responses that match the given predicate.
func (r Responses) Match(f func(r Response) bool) Responses {
	var matched []Response
	for _, rr := range r {
		if f(rr) {
			matched = append(matched, rr)
		}
	}
	return matched
}

func (r Responses) String() string {
	var out strings.Builder
	for i, resp := range r {
		out.WriteString(fmt.Sprintf("Response[%d]:\n%s", i, resp.String()))
	}
	return out.String()
}
