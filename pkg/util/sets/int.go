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

type IntSet map[int]struct{}

// NewIntSetWithLength returns an empty Set with the given capacity.
// It's only a hint, not a limitation.
func NewIntSetWithLength(l int) IntSet {
	return make(IntSet, l)
}

// NewIntSet creates a new Set with the given items.
func NewIntSet(items ...int) IntSet {
	s := NewIntSetWithLength(len(items))
	return s.InsertAll(items...)
}

// Insert a single item to this Set.
func (s IntSet) Insert(item int) IntSet {
	s[item] = struct{}{}
	return s
}

// InsertAll adds the items to this Set.
func (s IntSet) InsertAll(items ...int) IntSet {
	for _, item := range items {
		s.Insert(item)
	}
	return s
}

// Contains returns whether the given item is in the set.
func (s IntSet) Contains(item int) bool {
	_, ok := s[item]
	return ok
}
