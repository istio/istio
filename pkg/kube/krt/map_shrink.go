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

package krt

import "istio.io/istio/pkg/util/sets"

// shrinkingMap releases capacity after a tenfold contraction of a large map.
// Its zero value is ready to use. Callers provide synchronization and must use
// set and delete for mutations; data is exposed only for reads and iteration.
// Do not copy a shrinkingMap after use. Deleting the current key during
// iteration is supported, but other mutations may invalidate saved data aliases.
type shrinkingMap[K comparable, V any] struct {
	data map[K]V
	peak int
}

// newShrinkingMap takes ownership of data, including its existing entries.
func newShrinkingMap[K comparable, V any](data map[K]V) shrinkingMap[K, V] {
	return shrinkingMap[K, V]{data: data, peak: len(data)}
}

func (m *shrinkingMap[K, V]) set(key K, value V) {
	if m.data == nil {
		m.data = make(map[K]V)
	}
	m.data[key] = value
	m.peak = max(m.peak, len(m.data))
}

func (m *shrinkingMap[K, V]) delete(key K) {
	delete(m.data, key)
	if m.peak < 1024 || len(m.data) > m.peak/10 {
		return
	}
	smaller := make(map[K]V, len(m.data))
	for k, v := range m.data {
		smaller[k] = v
	}
	m.data = smaller
	m.peak = len(smaller)
}

// shrinkingMapOfSets maintains a map of sets, removing empty sets from the outer map.
// Its zero value is ready to use, with the same synchronization rules as shrinkingMap.
type shrinkingMapOfSets[K, V comparable] struct {
	shrinkingMap[K, sets.Set[V]]
}

func (m *shrinkingMapOfSets[K, V]) insert(key K, value V) {
	s := m.data[key]
	if s == nil {
		s = sets.New[V]()
		m.set(key, s)
	}
	s.Insert(value)
}

func (m *shrinkingMapOfSets[K, V]) delete(key K, value V) {
	s := m.data[key]
	s.Delete(value)
	if len(s) == 0 {
		m.shrinkingMap.delete(key)
	}
}
