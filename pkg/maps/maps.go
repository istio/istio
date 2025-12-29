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

package maps

import (
	"cmp"
	"iter"
	"maps"   // nolint: depguard
	"slices" // nolint: depguard
)

// Equal reports whether two maps contain the same key/value pairs.
// Values are compared using ==.
func Equal[M1, M2 ~map[K]V, K, V comparable](m1 M1, m2 M2) bool {
	return maps.Equal(m1, m2)
}

// Clone returns a copy of the map.
// The elements are copied using assignment, so this is a shallow clone.
func Clone[M ~map[K]V, K comparable, V any](m M) M {
	return maps.Clone(m)
}

// Values returns the values of the map m.
// The values will be in an indeterminate order.
func Values[M ~map[K]V, K comparable, V any](m M) []V {
	r := make([]V, 0, len(m))
	for _, v := range m {
		r = append(r, v)
	}
	return r
}

// Keys returns the keys of the map m.
// The keys will be in an indeterminate order.
func Keys[M ~map[K]V, K comparable, V any](m M) []K {
	r := make([]K, 0, len(m))
	for k := range m {
		r = append(r, k)
	}
	return r
}

// MergeCopy creates a new map by merging all key/value pairs from base and override.
// When a key in override is already present in base,
// the value in base will be overwritten by the value associated
// with the key in override.
func MergeCopy[M1 ~map[K]V, M2 ~map[K]V, K comparable, V any](base M1, override M2) M1 {
	dst := make(M1, len(base)+len(override))
	maps.Copy(dst, base)
	maps.Copy(dst, override)
	return dst
}

func Copy[M1 ~map[K]V, M2 ~map[K]V, K comparable, V any](dst M1, src M2) {
	maps.Copy(dst, src)
}

// Contains checks if all key-value pairs in 'subset' are present in 'superset'.
// It returns true only if every key in 'subset' exists in 'superset' and their corresponding values are equal.
func Contains[M1, M2 ~map[K]V, K comparable, V comparable](superset M1, subset M2) bool {
	for key, value := range subset {
		if supersetValue, ok := superset[key]; !ok || supersetValue != value {
			return false
		}
	}
	return true
}

// EqualFunc is like Equal, but compares values using eq.
// Keys are still compared with ==.
func EqualFunc[M1 ~map[K]V1, M2 ~map[K]V2, K comparable, V1, V2 any](m1 M1, m2 M2, eq func(V1, V2) bool) bool {
	return maps.EqualFunc(m1, m2, eq)
}

func SeqStable[M ~map[K]V, K cmp.Ordered, V any](m M) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		k := Keys(m)
		slices.Sort(k)
		for _, key := range k {
			if !yield(key, m[key]) {
				return
			}
		}
	}
}

func SeqStableBy[M ~map[K]V, K cmp.Ordered, V any](m M) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		k := Keys(m)
		slices.Sort(k)
		for _, key := range k {
			if !yield(key, m[key]) {
				return
			}
		}
	}
}
