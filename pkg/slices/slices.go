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
	"golang.org/x/exp/constraints"
	"golang.org/x/exp/slices"
)

// Equal reports whether two slices are equal: the same length and all
// elements equal. If the lengths are different, Equal returns false.
// Otherwise, the elements are compared in increasing index order, and the
// comparison stops at the first unequal pair.
// Floating point NaNs are not considered equal.
func Equal[E comparable](s1, s2 []E) bool {
	return slices.Equal(s1, s2)
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
func SortFunc[E any](x []E, less func(a, b E) bool) []E {
	if len(x) <= 1 {
		return x
	}
	slices.SortFunc(x, less)
	return x
}

// Sort sorts a slice of any ordered type in ascending order.
// The slice is modified in place but returned.
func Sort[E constraints.Ordered](x []E) []E {
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
	return slices.Delete(s, i, i+1)
}

// Contains reports whether v is present in s.
func Contains[E comparable](s []E, v E) bool {
	return slices.Contains(s, v)
}

// FindFunc finds the first element matching the function, or nil if none do
func FindFunc[E any](s []E, f func(E) bool) *E {
	idx := slices.IndexFunc(s, f)
	if idx == -1 {
		return nil
	}
	return &s[idx]
}

// Reverse returns its argument array reversed
func Reverse[E any](r []E) []E {
	for i, j := 0, len(r)-1; i < len(r)/2; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return r
}

// FilterInPlace retains all elements in []E that f(E) returns true for.
// The array is *mutated in place* and returned.
// Used Filter to avoid mutation
func FilterInPlace[E any](s []E, f func(E) bool) []E {
	n := 0
	for _, val := range s {
		if f(val) {
			s[n] = val
			n++
		}
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

// Dereference returns all non-nil references, derefernced
func Dereference[E any](s []*E) []E {
	res := make([]E, 0, len(s))
	for _, v := range s {
		if v != nil {
			res = append(res, *v)
		}
	}
	return res
}
