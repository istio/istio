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

type Set map[string]struct{}

// NewSet creates a Set from a list of values.
func NewSet(items ...string) Set {
	ss := Set{}
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

// UnsortedList returns the slice with contents in random order.
func (s Set) UnsortedList() []string {
	res := make([]string, 0, len(s))
	for key := range s {
		res = append(res, key)
	}
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
