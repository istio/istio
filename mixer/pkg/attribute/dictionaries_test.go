// Copyright 2016 Istio Authors
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

package attribute

import (
	"strconv"
	"testing"
)

func TestCompareDictionary(t *testing.T) {
	cases := []struct {
		d1             dictionary
		d2             dictionary
		expectedResult bool
	}{
		{make(dictionary), nil, true},
		{make(dictionary), make(dictionary), true},
		{dictionary{1: "1"}, nil, false},
		{dictionary{1: "1"}, dictionary{1: "1"}, true},
		{dictionary{1: "1"}, dictionary{1: "2"}, false},
		{dictionary{1: "1"}, dictionary{2: "1"}, false},
		{dictionary{1: "1"}, dictionary{2: "2"}, false},
		{dictionary{1: "1"}, dictionary{1: "1", 2: "1"}, false},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			r := compareDictionaries(c.d1, c.d2)
			if r != c.expectedResult {
				t.Errorf("Comparison returned %v, expected %v", r, c.expectedResult)
			}

			r = compareDictionaries(c.d2, c.d1)
			if r != c.expectedResult {
				t.Errorf("Comparison returned %v, expected %v", r, c.expectedResult)
			}
		})
	}
}

func TestDictionaries(t *testing.T) {
	mgr := dictionaries{}
	l := len(mgr.entries)
	if l != 0 {
		t.Errorf("Expected 0 entries, got %v", l)
	}

	// simple interning
	d1 := dictionary{1: "1", 2: "2"}
	d2 := mgr.Intern(d1)

	if !compareDictionaries(d1, d2) {
		t.Errorf("Didn't receive an equivalent dictionary from Intern")
	}

	l = len(mgr.entries)
	if l != 1 {
		t.Errorf("Expected 1 entry, got %v", l)
	}

	// intern again and make sure no new entry is added
	d2 = mgr.Intern(d1)
	if !compareDictionaries(d1, d2) {
		t.Errorf("Didn't receive an equivalent dictionary from Intern")
	}

	l = len(mgr.entries)
	if l != 1 {
		t.Errorf("Expected 1 entry, got %v", l)
	}

	// intern something different and make sure a new entry is created
	d3 := dictionary{3: "3", 4: "4"}
	d4 := mgr.Intern(d3)

	if !compareDictionaries(d3, d4) {
		t.Errorf("Didn't receive an equivalent dictionary from Intern")
	}

	l = len(mgr.entries)
	if l != 2 {
		t.Errorf("Expected 2 entries, got %v", l)
	}

	// remove one use, make sure the entry count stays the same
	mgr.Release(d1)
	l = len(mgr.entries)
	if l != 2 {
		t.Errorf("Expected 2 entries, got %v", l)
	}

	// remove another use, make sure the entry count drops to 1
	mgr.Release(d1)
	l = len(mgr.entries)
	if l != 1 {
		t.Errorf("Expected 1 entries, got %v", l)
	}

	// remove the last use
	mgr.Release(d3)
	l = len(mgr.entries)
	if l != 0 {
		t.Errorf("Expected 0 entries, got %v", l)
	}
}
