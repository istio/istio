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

package smallset

import (
	"cmp"
	"fmt"

	"istio.io/istio/pkg/slices"
)

// Set is an immutable set optimized for *small number of items*. For general purposes, Sets is likely better
//
// *Set construction*: sets is roughly 1kb allocations per 250 items. smallsets is 0.
// *Contains* sets is O(1). smallsets is O(logn). smallsets is typically faster up to about 5 elements.
//
//	At 1000 items, it is roughly 5x slower (30ns vs 5ns).
type Set[T cmp.Ordered] struct {
	items []T
}

// NewPresorted creates a new Set with the given items.
// If items is not sorted or contains duplicates, this gives undefined behavior; use New instead.
func NewPresorted[T cmp.Ordered](items ...T) Set[T] {
	return Set[T]{items: items}
}

// New creates a new Set with the given items.
// Duplicates are removed
func New[T cmp.Ordered](items ...T) Set[T] {
	if len(items) == 1 {
		return Set[T]{items: items}
	}
	slices.Sort(items)
	items = slices.FilterDuplicatesPresorted(items)
	return Set[T]{items: items}
}

// CopyAndInsert builds a *new* with all the current items plus new items
func (s Set[T]) CopyAndInsert(items ...T) Set[T] {
	slices.Sort(items)
	// This is basically the 'merge' part of merge sort.
	a := s.items
	b := items
	nl := make([]T, 0, len(a)+len(b))
	i, j := 0, 0
	appendIfUnique := func(t T) []T {
		l := len(nl)
		if l == 0 {
			nl = append(nl, t)
		} else {
			last := nl[l-1]
			if last != t {
				nl = append(nl, t)
			}
		}
		return nl
	}
	for i < len(a) && j < len(b) {
		if a[i] < b[j] {
			nl = appendIfUnique(a[i])
			i++
		} else {
			nl = appendIfUnique(b[j])
			j++
		}
	}
	for ; i < len(a); i++ {
		nl = appendIfUnique(a[i])
	}
	for ; j < len(b); j++ {
		nl = appendIfUnique(b[j])
	}
	// we already know they are sorted+unique
	return Set[T]{items: nl}
}

// List returns the underlying slice. Must not be modified
func (s Set[T]) List() []T {
	return s.items
}

// Contains returns whether the given item is in the set.
func (s Set[T]) Contains(item T) bool {
	_, f := slices.BinarySearch(s.items, item)
	return f
}

// Len returns the number of elements in this Set.
func (s Set[T]) Len() int {
	return len(s.items)
}

// IsEmpty indicates whether the set is the empty set.
func (s Set[T]) IsEmpty() bool {
	return len(s.items) == 0
}

// IsNil indicates whether the set is nil. This is different from an empty set.
// 'var smallset.Set': nil
// smallset.New(): nil
// smallset.New(emptyList...): not nil
func (s Set[T]) IsNil() bool {
	return s.items == nil
}

// String returns a string representation of the set.
// Use it only for debugging and logging.
func (s Set[T]) String() string {
	return fmt.Sprintf("%v", s.items)
}
