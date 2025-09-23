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

import (
	"cmp"
	"fmt"

	"istio.io/istio/pkg/slices"
)

type Set[T comparable] map[T]struct{}

type String = Set[string]

// NewWithLength returns an empty Set with the given capacity.
// It's only a hint, not a limitation.
func NewWithLength[T comparable](l int) Set[T] {
	return make(Set[T], l)
}

// New creates a new Set with the given items.
func New[T comparable](items ...T) Set[T] {
	s := NewWithLength[T](len(items))
	return s.InsertAll(items...)
}

// Insert a single item to this Set.
func (s Set[T]) Insert(item T) Set[T] {
	s[item] = struct{}{}
	return s
}

// InsertAll adds the items to this Set.
func (s Set[T]) InsertAll(items ...T) Set[T] {
	for _, item := range items {
		s[item] = struct{}{}
	}
	return s
}

// Delete removes an item from the set.
func (s Set[T]) Delete(item T) Set[T] {
	delete(s, item)
	return s
}

// DeleteAll removes items from the set.
func (s Set[T]) DeleteAll(items ...T) Set[T] {
	for _, item := range items {
		delete(s, item)
	}
	return s
}

// DeleteAllSet removes items from the set.
// Note: this differs from Difference() as this is in-place
func (s Set[T]) DeleteAllSet(other Set[T]) Set[T] {
	for item := range other {
		delete(s, item)
	}
	return s
}

// Merge a set of objects that are in s2 into s
// For example:
// s = {a1, a2, a3}
// s2 = {a3, a4, a5}
// s.Merge(s2) = {a1, a2, a3, a4, a5}
func (s Set[T]) Merge(s2 Set[T]) Set[T] {
	for item := range s2 {
		s[item] = struct{}{}
	}

	return s
}

// Copy this set.
func (s Set[T]) Copy() Set[T] {
	result := NewWithLength[T](s.Len())
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
func (s Set[T]) Union(s2 Set[T]) Set[T] {
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
func (s Set[T]) Difference(s2 Set[T]) Set[T] {
	result := New[T]()
	for key := range s {
		if !s2.Contains(key) {
			result.Insert(key)
		}
	}
	return result
}

// DifferenceInPlace similar to Difference, but has better performance.
// Note: This function modifies s in place.
func (s Set[T]) DifferenceInPlace(s2 Set[T]) Set[T] {
	for key := range s {
		if s2.Contains(key) {
			delete(s, key)
		}
	}
	return s
}

// Diff takes a pair of Sets, and returns the elements that occur only on the left and right set.
func (s Set[T]) Diff(other Set[T]) (left []T, right []T) {
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
	return left, right
}

// Intersection returns a set of objects that are common between s and s2
// For example:
// s = {a1, a2, a3}
// s2 = {a1, a2, a4, a5}
// s.Intersection(s2) = {a1, a2}
func (s Set[T]) Intersection(s2 Set[T]) Set[T] {
	result := New[T]()
	for key := range s {
		if s2.Contains(key) {
			result.Insert(key)
		}
	}
	return result
}

// IntersectInPlace similar to Intersection, but has better performance.
// Note: This function modifies s in place.
func (s Set[T]) IntersectInPlace(s2 Set[T]) Set[T] {
	for key := range s {
		if !s2.Contains(key) {
			delete(s, key)
		}
	}
	return s
}

// SupersetOf returns true if s contains all elements of s2
// For example:
// s = {a1, a2, a3}
// s2 = {a1, a2, a3, a4, a5}
// s.SupersetOf(s2) = false
// s2.SupersetOf(s) = true
func (s Set[T]) SupersetOf(s2 Set[T]) bool {
	if s2 == nil {
		return true
	}
	if len(s2) > len(s) {
		return false
	}
	for key := range s2 {
		if !s.Contains(key) {
			return false
		}
	}
	return true
}

// UnsortedList returns the slice with contents in random order.
func (s Set[T]) UnsortedList() []T {
	res := make([]T, 0, s.Len())
	for key := range s {
		res = append(res, key)
	}
	return res
}

// SortedList returns the slice with contents sorted.
func SortedList[T cmp.Ordered](s Set[T]) []T {
	res := s.UnsortedList()
	slices.Sort(res)
	return res
}

// InsertContains inserts the item into the set and returns if it was already present.
// Example:
//
//		if !set.InsertContains(item) {
//			fmt.Println("Added item for the first time", item)
//	  }
func (s Set[T]) InsertContains(item T) bool {
	if s.Contains(item) {
		return true
	}
	s[item] = struct{}{}
	return false
}

// DeleteContains deletes the item from the set and returns if it was already present.
// Example:
//
//	if set.DeleteContains(item) {
//		fmt.Println("item was delete", item)
//	}
func (s Set[T]) DeleteContains(item T) bool {
	if !s.Contains(item) {
		return false
	}
	delete(s, item)
	return true
}

// Contains returns whether the given item is in the set.
func (s Set[T]) Contains(item T) bool {
	_, ok := s[item]
	return ok
}

// ContainsAll is alias of SupersetOf
// returns true if s contains all elements of s2
func (s Set[T]) ContainsAll(s2 Set[T]) bool {
	return s.SupersetOf(s2)
}

// Equals checks whether the given set is equal to the current set.
func (s Set[T]) Equals(other Set[T]) bool {
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
func (s Set[T]) Len() int {
	return len(s)
}

// IsEmpty indicates whether the set is the empty set.
func (s Set[T]) IsEmpty() bool {
	return len(s) == 0
}

// String returns a string representation of the set.
// Be aware that the order of elements is random so the string representation may vary.
// Use it only for debugging and logging.
func (s Set[T]) String() string {
	return fmt.Sprintf("%v", s.UnsortedList())
}

// InsertOrNew inserts t into the set if the set exists, or returns a new set with t if not.
// Works well with DeleteCleanupLast.
// Example:
//
//	InsertOrNew(m, key, value)
func InsertOrNew[K comparable, T comparable](m map[K]Set[T], k K, v T) {
	s, f := m[k]
	if !f {
		m[k] = New(v)
	} else {
		s.Insert(v)
	}
}

// DeleteCleanupLast removes an element from a set in a map of sets, deleting the key from the map if there are no keys left.
// Works well with InsertOrNew.
// Example:
//
//	sets.DeleteCleanupLast(m, key, value)
func DeleteCleanupLast[K comparable, T comparable](m map[K]Set[T], k K, v T) {
	if m[k].Delete(v).IsEmpty() {
		delete(m, k)
	}
}
