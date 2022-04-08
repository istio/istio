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

package sets

import "sort"

type Set map[string]struct{}

// NewWithLength returns an empty Set with the given capacity.
// It's only a hint, not a limitation.
func NewWithLength(l int) Set {
	return make(Set, l)
}

// New creates a new Set with the given items.
func New(items ...string) Set {
	s := NewWithLength(len(items))
	return s.InsertAll(items...)
}

// Insert a single item to this Set.
func (s Set) Insert(item string) Set {
	s[item] = struct{}{}
	return s
}

// InsertAll adds the items to this Set.
func (s Set) InsertAll(items ...string) Set {
	for _, item := range items {
		s[item] = struct{}{}
	}
	return s
}

// Delete removes items from the set.
func (s Set) Delete(items ...string) Set {
	for _, item := range items {
		delete(s, item)
	}
	return s
}

// Merge a set of objects that are in s2 into s
// For example:
// s = {a1, a2, a3}
// s2 = {a3, a4, a5}
// s.Merge(s2) = {a1, a2, a3, a4, a5}
func (s Set) Merge(s2 Set) Set {
	for item := range s2 {
		s[item] = struct{}{}
	}

	return s
}

// Copy this set.
func (s Set) Copy() Set {
	result := New()
	for key := range s {
		result.Insert(key)
	}
	return result
}

// Union returns a set of objects that are in s or s2
// For example:
// s = {a1, a2, a3}
// s2 = {a1, a2, a4, a5}
// s.Union(s2) = s2.Union(s) = {a1, a2, a3, a4, a5}
func (s Set) Union(s2 Set) Set {
	result := s.Copy()
	for key := range s2 {
		result.Insert(key)
	}
	return result
}

// Difference returns a set of objects that are not in s2
// For example:
// s = {a1, a2, a3}
// s2 = {a1, a2, a4, a5}
// s.Difference(s2) = {a3}
// s2.Difference(s) = {a4, a5}
func (s Set) Difference(s2 Set) Set {
	result := New()
	for key := range s {
		if !s2.Contains(key) {
			result.Insert(key)
		}
	}
	return result
}

// Intersection returns a set of objects that are common between s and s2
// For example:
// s = {a1, a2, a3}
// s2 = {a1, a2, a4, a5}
// s.Intersection(s2) = {a1, a2}
func (s Set) Intersection(s2 Set) Set {
	result := New()
	for key := range s {
		if s2.Contains(key) {
			result.Insert(key)
		}
	}
	return result
}

// SupersetOf returns true if s contains all elements of s2
// For example:
// s = {a1, a2, a3}
// s2 = {a1, a2, a3, a4, a5}
// s.SupersetOf(s2) = false
// s2.SupersetOf(s) = true
func (s Set) SupersetOf(s2 Set) bool {
	return s2.Difference(s).IsEmpty()
}

// UnsortedList returns the slice with contents in random order.
func (s Set) UnsortedList() []string {
	res := make([]string, 0, s.Len())
	for key := range s {
		res = append(res, key)
	}
	return res
}

// SortedList returns the slice with contents sorted.
func (s Set) SortedList() []string {
	res := s.UnsortedList()
	sort.Strings(res)
	return res
}

// Contains returns whether the given item is in the set.
func (s Set) Contains(item string) bool {
	_, ok := s[item]
	return ok
}

// Equals checks whether the given set is equal to the current set.
func (s Set) Equals(other Set) bool {
	if s.Len() != other.Len() {
		return false
	}

	for key := range s {
		if !other.Contains(key) {
			return false
		}
	}

	return true
}

// Len returns the number of elements in this Set.
func (s Set) Len() int {
	return len(s)
}

// IsEmpty indicates whether the set is the empty set.
func (s Set) IsEmpty() bool {
	return len(s) == 0
}

// Diff takes a pair of Sets, and returns the elements that occur only on the left and right set.
func (s Set) Diff(other Set) (left []string, right []string) {
	for k := range s {
		if _, f := other[k]; !f {
			left = append(left, k)
		}
	}
	for k := range other {
		if _, f := s[k]; !f {
			right = append(right, k)
		}
	}
	return
}
