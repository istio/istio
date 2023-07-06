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

import "golang.org/x/exp/maps"

// Equal reports whether two maps contain the same key/value pairs.
// Values are compared using ==.
func Equal[M1, M2 ~map[K]V, K, V comparable](m1 M1, m2 M2) bool {
	return maps.Equal(m1, m2)
}

// Clone returns a copy of the slice.
// The elements are copied using assignment, so this is a shallow clone.
func Clone[M ~map[K]V, K comparable, V any](m M) M {
	return maps.Clone(m)
}

// Values returns the values of the map m.
// The values will be in an indeterminate order.
func Values[M ~map[K]V, K comparable, V any](m M) []V {
	return maps.Values(m)
}

// Keys returns the keys of the map m.
// The keys will be in an indeterminate order.
func Keys[M ~map[K]V, K comparable, V any](m M) []K {
	return maps.Keys(m)
}

// MergeCopy creates a new map by merging all key/value pairs from base and override.
// When a key in override is already present in base,
// the value in base will be overwritten by the value associated
// with the key in override.
func MergeCopy[M1 ~map[K]V, M2 ~map[K]V, K comparable, V any](base M1, override M2) M1 {
	dst := make(M1)
	maps.Copy(dst, base)
	maps.Copy(dst, override)
	return dst
}
