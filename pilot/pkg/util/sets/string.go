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

// NewSet creates a Set from a list of values.
func NewSet(items ...string) Set {
	ss := make(Set, len(items))
	ss.Insert(items...)
	return ss
}

// Insert adds items to the set.
func (s Set) Insert(items ...string) Set {
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

// Union returns a set of objects that are in s or s2
// For example:
// s = {a1, a2, a3}
// s2 = {a1, a2, a4, a5}
// s.Union(s2) = s2.Union(s) = {a1, a2, a3, a4, a5}
func (s Set) Union(s2 Set) Set {
	result := NewSet()
	for key := range s {
		result.Insert(key)
	}
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
	result := NewSet()
	for key := range s {
		if _, exist := s2[key]; !exist {
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
	result := NewSet()
	for key := range s {
		if _, exist := s2[key]; exist {
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
	return len(s2.Difference(s)) == 0
}

// UnsortedList returns the slice with contents in random order.
func (s Set) UnsortedList() []string {
	res := make([]string, 0, len(s))
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
	if len(s) != len(other) {
		return false
	}

	for key := range s {
		if _, exists := other[key]; !exists {
			return false
		}
	}

	return true
}

// Empty returns whether the set is the empty set.
func (s Set) Empty() bool {
	return len(s) == 0
}
