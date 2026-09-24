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

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

func TestShrinkingMap(t *testing.T) {
	var m shrinkingMap[int, int]
	m.delete(0) // The zero value supports both deletion and insertion.
	for i := range 2000 {
		m.set(i, i)
	}
	for i := 2000; i > 201; i-- {
		m.delete(i - 1)
	}
	assert.Equal(t, m.peak, 2000)
	old := m.data
	m.delete(200)
	assert.Equal(t, m.peak, 200)
	assert.Equal(t, len(m.data), 200)
	for k, v := range m.data {
		assert.Equal(t, k, v)
	}
	old[-1] = -1
	_, retained := m.data[-1]
	assert.Equal(t, retained, false)
	// Small maps retain capacity through ordinary churn.
	for k := range m.data {
		m.delete(k)
	}
	assert.Equal(t, m.peak, 200)
	// A second growth/shrink cycle still releases capacity.
	for i := range 2000 {
		m.set(i, i)
	}
	old = m.data
	for k := range m.data {
		m.delete(k)
	}
	old[-1] = -1
	assert.Equal(t, len(m.data), 0)
	assert.Equal(t, m.peak, 200)
}

func TestShrinkingMapOfSets(t *testing.T) {
	var m shrinkingMapOfSets[int, int]
	for i := range 2000 {
		m.insert(i, 1)
		m.insert(i, 2)
	}
	old := m.data
	for i := range 2000 {
		m.delete(i, 1)
		assert.Equal(t, m.data[i].Contains(2), true)
		m.delete(i, 2)
		_, found := m.data[i]
		assert.Equal(t, found, false)
	}
	old[-1] = sets.New(1)
	assert.Equal(t, len(m.data), 0)
	assert.Equal(t, m.peak, 200)
	m.delete(-1, 1)
}

func TestCollectionShrinkKeepsIndexHandles(t *testing.T) {
	stop := test.NewStop(t)
	values := make([]Named, 2048)
	for i := range values {
		values[i] = Named{Name: fmt.Sprint(i)}
	}
	source := NewMutableCollection(nil, values, WithStop(stop))
	sourceIndex := source.index("name", func(v Named) []string { return []string{v.Name} })
	derived := NewCollection(source.AsCollection(), func(_ HandlerContext, v Named) *Named { return &v }, WithStop(stop))
	derived.WaitUntilSynced(stop)
	mc := derived.internal().(*manyCollection[Named, Named])
	derivedIndex := mc.index("name", func(v Named) []string { return []string{v.Name} })
	source.DeleteObjects(func(v Named) bool { return v.Name != "0" })
	assert.EventuallyEqual(t, func() int { return len(derived.List()) }, 1)
	assert.Equal(t, sourceIndex.Lookup("0"), []Named{{Name: "0"}})
	assert.Equal(t, derivedIndex.Lookup("0"), []Named{{Name: "0"}})
	source.UpdateObject(Named{Name: "new"})
	assert.EventuallyEqual(t, func() int { return len(derivedIndex.Lookup("new")) }, 1)
	assert.Equal(t, sourceIndex.Lookup("new"), []Named{{Name: "new"}})
	mc.mu.RLock()
	assert.Equal(t, mc.collectionState.inputs.peak < 1024, true)
	assert.Equal(t, mc.collectionState.outputs.peak < 1024, true)
	assert.Equal(t, mc.collectionState.mappings.peak < 1024, true)
	mc.mu.RUnlock()
}
