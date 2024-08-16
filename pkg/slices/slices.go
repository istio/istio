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

// Package slices defines various functions useful with slices of any type.
package slices

import (
	"cmp"
	"slices" // nolint: depguard
	"strings"
)

// Equal reports whether two slices are equal: the same length and all
// elements equal. If the lengths are different, Equal returns false.
// Otherwise, the elements are compared in increasing index order, and the
// comparison stops at the first unequal pair.
// Floating point NaNs are not considered equal.
func Equal[E comparable](s1, s2 []E) bool {
	return slices.Equal(s1, s2)
}

// EqualUnordered reports whether two slices are equal, ignoring order
func EqualUnordered[E comparable](s1, s2 []E) bool {
	if len(s1) != len(s2) {
		return false
	}
	first := make(map[E]struct{}, len(s1))
	for _, c := range s1 {
		first[c] = struct{}{}
	}
	for _, c := range s2 {
		if _, f := first[c]; !f {
			return false
		}
	}
	return true
}

// EqualFunc reports whether two slices are equal using a comparison
// function on each pair of elements. If the lengths are different,
// EqualFunc returns false. Otherwise, the elements are compared in
// increasing index order, and the comparison stops at the first index
// for which eq returns false.
func EqualFunc[E1, E2 comparable](s1 []E1, s2 []E2, eq func(E1, E2) bool) bool {
	return slices.EqualFunc(s1, s2, eq)
}

// SortFunc sorts the slice x in ascending order as determined by the less function.
// This sort is not guaranteed to be stable.
// The slice is modified in place but returned.
func SortFunc[E any](x []E, less func(a, b E) int) []E {
	if len(x) <= 1 {
		return x
	}
	slices.SortFunc(x, less)
	return x
}

// SortStableFunc sorts the slice x while keeping the original order of equal element.
// The slice is modified in place but returned.
// Please refer to SortFunc for usage instructions.
func SortStableFunc[E any](x []E, less func(a, b E) int) []E {
	if len(x) <= 1 {
		return x
	}
	slices.SortStableFunc(x, less)
	return x
}

// SortBy is a helper to sort a slice by some value. Typically, this would be sorting a struct
// by a single field. If you need to have multiple fields, see the ExampleSort.
func SortBy[E any, A cmp.Ordered](x []E, extract func(a E) A) []E {
	if len(x) <= 1 {
		return x
	}
	SortFunc(x, func(a, b E) int {
		return cmp.Compare(extract(a), extract(b))
	})
	return x
}

// Sort sorts a slice of any ordered type in ascending order.
// The slice is modified in place but returned.
func Sort[E cmp.Ordered](x []E) []E {
	if len(x) <= 1 {
		return x
	}
	slices.Sort(x)
	return x
}

// Clone returns a copy of the slice.
// The elements are copied using assignment, so this is a shallow clone.
func Clone[S ~[]E, E any](s S) S {
	return slices.Clone(s)
}

// Delete removes the element i from s, returning the modified slice.
func Delete[S ~[]E, E any](s S, i int) S {
	// Since Go 1.22, "slices.Delete zeroes the elements s[len(s)-(j-i):len(s)]"
	// (no memory leak)
	return slices.Delete(s, i, i+1)
}

// Contains reports whether v is present in s.
func Contains[E comparable](s []E, v E) bool {
	return slices.Contains(s, v)
}

// Max returns the maximal value in x. It panics if x is empty.
// For floating-point E, Max propagates NaNs (any NaN value in x
// forces the output to be NaN).
func Max[S ~[]E, E cmp.Ordered](x S) E {
	return slices.Max(x)
}

// FindFunc finds the first element matching the function, or nil if none do
func FindFunc[E any](s []E, f func(E) bool) *E {
	idx := slices.IndexFunc(s, f)
	if idx == -1 {
		return nil
	}
	return &s[idx]
}

// First returns the first item in the slice, if there is one
func First[E any](s []E) *E {
	if len(s) == 0 {
		return nil
	}
	return &s[0]
}

// Reverse returns its argument array reversed
func Reverse[E any](r []E) []E {
	for i, j := 0, len(r)-1; i < len(r)/2; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return r
}

func BinarySearch[S ~[]E, E cmp.Ordered](x S, target E) (int, bool) {
	return slices.BinarySearch(x, target)
}

// FilterInPlace retains all elements in []E that f(E) returns true for.
// The array is *mutated in place* and returned.
// Use Filter to avoid mutation
func FilterInPlace[E any](s []E, f func(E) bool) []E {
	n := 0
	for _, val := range s {
		if f(val) {
			s[n] = val
			n++
		}
	}

	// If those elements contain pointers you might consider zeroing those elements
	// so that objects they reference can be garbage collected."
	var empty E
	for i := n; i < len(s); i++ {
		s[i] = empty
	}

	s = s[:n]
	return s
}

// FilterDuplicatesPresorted retains all unique elements in []E.
// The slices MUST be pre-sorted.
func FilterDuplicatesPresorted[E comparable](s []E) []E {
	if len(s) <= 1 {
		return s
	}
	n := 1
	for i := 1; i < len(s); i++ {
		val := s[i]
		if val != s[i-1] {
			s[n] = val
			n++
		}
	}

	// If those elements contain pointers you might consider zeroing those elements
	// so that objects they reference can be garbage collected."
	var empty E
	for i := n; i < len(s); i++ {
		s[i] = empty
	}

	s = s[:n]
	return s
}

// Filter retains all elements in []E that f(E) returns true for.
// A new slice is created and returned. Use FilterInPlace to perform in-place
func Filter[E any](s []E, f func(E) bool) []E {
	matched := []E{}
	for _, v := range s {
		if f(v) {
			matched = append(matched, v)
		}
	}
	return matched
}

// Map runs f() over all elements in s and returns the result
func Map[E any, O any](s []E, f func(E) O) []O {
	n := make([]O, 0, len(s))
	for _, e := range s {
		n = append(n, f(e))
	}
	return n
}

// MapErr runs f() over all elements in s and returns the result, short circuiting if there is an error.
func MapErr[E any, O any](s []E, f func(E) (O, error)) ([]O, error) {
	n := make([]O, 0, len(s))
	for _, e := range s {
		res, err := f(e)
		if err != nil {
			return nil, err
		}
		n = append(n, res)
	}
	return n, nil
}

// MapFilter runs f() over all elements in s and returns any non-nil results
func MapFilter[E any, O any](s []E, f func(E) *O) []O {
	n := make([]O, 0, len(s))
	for _, e := range s {
		if res := f(e); res != nil {
			n = append(n, *res)
		}
	}
	return n
}

// Reference takes a pointer to all elements in the slice
func Reference[E any](s []E) []*E {
	res := make([]*E, 0, len(s))
	for _, v := range s {
		v := v
		res = append(res, &v)
	}
	return res
}

// Dereference returns all non-nil references, dereferenced
func Dereference[E any](s []*E) []E {
	res := make([]E, 0, len(s))
	for _, v := range s {
		if v != nil {
			res = append(res, *v)
		}
	}
	return res
}

// Flatten merges a slice of slices into a single slice.
func Flatten[E any](s [][]E) []E {
	if s == nil {
		return nil
	}
	res := make([]E, 0)
	for _, v := range s {
		res = append(res, v...)
	}
	return res
}

// Group groups a slice by a key.
func Group[T any, K comparable](data []T, f func(T) K) map[K][]T {
	res := make(map[K][]T, len(data))
	for _, e := range data {
		k := f(e)
		res[k] = append(res[k], e)
	}
	return res
}

// GroupUnique groups a slice by a key. Each key must be unique or data will be lost. To allow multiple use Group.
func GroupUnique[T any, K comparable](data []T, f func(T) K) map[K]T {
	res := make(map[K]T, len(data))
	for _, e := range data {
		res[f(e)] = e
	}
	return res
}

func Join(sep string, fields ...string) string {
	return strings.Join(fields, sep)
}

// Insert inserts the values v... into s at index i,
// returning the modified slice.
// The elements at s[i:] are shifted up to make room.
// In the returned slice r, r[i] == v[0],
// and r[i+len(v)] == value originally at r[i].
// Insert panics if i is out of range.
// This function is O(len(s) + len(v)).
func Insert[S ~[]E, E any](s S, i int, v ...E) S {
	return slices.Insert(s, i, v...)
}
