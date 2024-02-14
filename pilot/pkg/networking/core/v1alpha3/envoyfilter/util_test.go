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

package envoyfilter

import (
	"testing"

	"istio.io/istio/pkg/slices"
)

func TestReplaceAndInsert(t *testing.T) {
	f := func(e int) (bool, int) {
		// replace 1 with 10
		if e == 1 {
			return true, 10
		}
		return false, 0
	}

	cases := []struct {
		name         string
		input        []int
		replace      []int
		insertBefore []int
		insertAfter  []int
		applied      bool
	}{
		{
			name:         "nil slice",
			input:        nil,
			replace:      nil,
			insertBefore: nil,
			insertAfter:  nil,
			applied:      false,
		},
		{
			name:         "the first",
			input:        []int{1, 2, 3},
			replace:      []int{10, 2, 3},
			insertBefore: []int{10, 1, 2, 3},
			insertAfter:  []int{1, 10, 2, 3},
			applied:      true,
		},
		{
			name:         "the middle",
			input:        []int{0, 1, 2, 3},
			replace:      []int{0, 10, 2, 3},
			insertBefore: []int{0, 10, 1, 2, 3},
			insertAfter:  []int{0, 1, 10, 2, 3},
			applied:      true,
		},
		{
			name:         "the last",
			input:        []int{3, 2, 1},
			replace:      []int{3, 2, 10},
			insertBefore: []int{3, 2, 10, 1},
			insertAfter:  []int{3, 2, 1, 10},
			applied:      true,
		},
		{
			name:         "match multiple",
			input:        []int{1, 2, 1},
			replace:      []int{10, 2, 1},
			insertBefore: []int{10, 1, 2, 1},
			insertAfter:  []int{1, 10, 2, 1},
			applied:      true,
		},
		{
			name:         "not exists",
			input:        []int{2, 3},
			replace:      []int{2, 3},
			insertBefore: []int{2, 3},
			insertAfter:  []int{2, 3},
			applied:      false,
		},
	}

	// all the test cases are to find the position of the number 1 in the slice,
	// and then replace (or insert before, or insert after) it with 10.
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// replaceFunc
			got, applied := replaceFunc(slices.Clone(c.input), f)
			if !slices.Equal(c.replace, got) {
				t.Errorf("replaceFunc: want %+v but got %+v", c.replace, got)
			}
			if c.applied != applied {
				t.Errorf("replaceFunc: want %+v but got %+v", c.applied, applied)
			}

			// insertBeforeFunc
			got, applied = insertBeforeFunc(slices.Clone(c.input), f)
			if !slices.Equal(c.insertBefore, got) {
				t.Errorf("insertBeforeFunc: want %+v but got %+v", c.insertBefore, got)
			}
			if c.applied != applied {
				t.Errorf("insertBeforeFunc: want %+v but got %+v", c.applied, applied)
			}

			// insertAfterFunc
			got, applied = insertAfterFunc(slices.Clone(c.input), f)
			if !slices.Equal(c.insertAfter, got) {
				t.Errorf("insertAfterFunc: want %+v but got %+v", c.insertAfter, got)
			}
			if c.applied != applied {
				t.Errorf("insertAfterFunc: want %+v but got %+v", c.applied, applied)
			}
		})
	}
}
