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

package smallset_test

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/util/smallset"
)

func TestSet(t *testing.T) {
	elements := []string{"d", "b", "a", "d"}
	set := smallset.New(elements...)

	assert.Equal(t, set.Len(), 3)
	assert.Equal(t, set.List(), []string{"a", "b", "d"})

	assert.Equal(t, set.Contains("a"), true)
	assert.Equal(t, set.Contains("b"), true)
	assert.Equal(t, set.Contains("c"), false)
	assert.Equal(t, set.Contains("d"), true)
	assert.Equal(t, set.Contains("e"), false)

	nset := set.CopyAndInsert("z", "c", "a")
	// Should not mutate original set
	assert.Equal(t, set.List(), []string{"a", "b", "d"})
	assert.Equal(t, nset.List(), []string{"a", "b", "c", "d", "z"})

	assert.Equal(t, nset.Contains("a"), true)
	assert.Equal(t, nset.Contains("b"), true)
	assert.Equal(t, nset.Contains("c"), true)
	assert.Equal(t, nset.Contains("d"), true)
	assert.Equal(t, nset.Contains("e"), false)
	assert.Equal(t, nset.Contains("z"), true)
}

func TestNew(t *testing.T) {
	var uninit smallset.Set[string]
	assert.Equal(t, uninit.IsNil(), true)
	assert.Equal(t, uninit.IsEmpty(), true)
	empty := smallset.New[string]()
	assert.Equal(t, empty.IsNil(), true)
	assert.Equal(t, empty.IsEmpty(), true)
	empty2 := smallset.New[string]([]string{}...)
	assert.Equal(t, empty2.IsNil(), false)
	assert.Equal(t, empty2.IsEmpty(), true)
}

func BenchmarkSet(b *testing.B) {
	items1000 := []string{}
	for i := 0; i < 1000; i++ {
		items1000 = append(items1000, fmt.Sprint(i))
	}
	items100 := []string{}
	for i := 0; i < 100; i++ {
		items100 = append(items100, fmt.Sprint(i))
	}
	items2 := []string{}
	for i := 0; i < 2; i++ {
		items2 = append(items2, fmt.Sprint(i))
	}
	b.Run("Set", func(b *testing.B) {
		set1000 := sets.New(items1000...)
		set100 := sets.New(items100...)
		set2 := sets.New(items2...)
		b.Run("New/1", func(b *testing.B) {
			for range b.N {
				_ = sets.New("a")
			}
		})
		b.Run("New/1000", func(b *testing.B) {
			for range b.N {
				_ = sets.New(items1000...)
			}
		})
		b.Run("Contains/1000", func(b *testing.B) {
			for range b.N {
				// Check an item in and out of the set
				_ = set1000.Contains("456")
				_ = set1000.Contains("abc")
			}
		})
		b.Run("Contains/100", func(b *testing.B) {
			for range b.N {
				_ = set100.Contains("45")
				_ = set100.Contains("abc")
			}
		})
		b.Run("Contains/2", func(b *testing.B) {
			for range b.N {
				_ = set2.Contains("1")
				_ = set2.Contains("abc")
			}
		})
	})
	b.Run("SmallSet", func(b *testing.B) {
		set1000 := smallset.New(items1000...)
		set100 := smallset.New(items100...)
		set2 := smallset.New(items2...)
		b.Run("New/1", func(b *testing.B) {
			for range b.N {
				_ = smallset.New("a")
			}
		})
		b.Run("New/1000", func(b *testing.B) {
			for range b.N {
				_ = smallset.New(items1000...)
			}
		})
		b.Run("NewPresorted/1000", func(b *testing.B) {
			for range b.N {
				_ = smallset.NewPresorted(items1000...)
			}
		})
		b.Run("Contains/1000", func(b *testing.B) {
			for range b.N {
				// Check an item in and out of the set
				_ = set1000.Contains("456")
				_ = set1000.Contains("abc")
			}
		})
		b.Run("Contains/100", func(b *testing.B) {
			for range b.N {
				// Check an item in and out of the set
				_ = set100.Contains("45")
				_ = set100.Contains("abc")
			}
		})
		b.Run("Contains/2", func(b *testing.B) {
			for range b.N {
				// Check an item in and out of the set
				_ = set2.Contains("1")
				_ = set2.Contains("abc")
			}
		})
	})
}
