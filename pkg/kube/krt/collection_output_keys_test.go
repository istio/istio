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
	"strconv"
	"testing"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestCollectionOutputKeyTransitions(t *testing.T) {
	stop := test.NewStop(t)
	type input struct {
		Named
		Keys []string
	}
	parents := NewMutableCollection[input](nil, nil, WithStop(stop))
	derived := NewManyCollection(parents.AsCollection(), func(_ HandlerContext, i input) []Named {
		result := make([]Named, 0, len(i.Keys))
		for _, key := range i.Keys {
			result = append(result, Named{Name: key})
		}
		return result
	}, WithStop(stop))
	derived.WaitUntilSynced(stop)
	mc := derived.internal().(*manyCollection[input, Named])
	large := make([]string, 2048)
	for i := range large {
		large[i] = strconv.Itoa(i)
	}
	for _, keys := range [][]string{{""}, {"a", "b"}, large, large[:200], {"a", "b"}, {"c"}, {}, {"d"}} {
		parents.UpdateObject(input{Named: Named{Name: "parent"}, Keys: keys})
		assert.EventuallyEqual(t, func() bool {
			mc.mu.RLock()
			defer mc.mu.RUnlock()
			if len(mc.collectionState.outputs.data) != len(keys) {
				return false
			}
			for _, key := range keys {
				if _, ok := mc.collectionState.outputs.data[getTypedKey(Named{Name: key})]; !ok {
					return false
				}
			}
			return true
		}, true)
		mc.mu.RLock()
		mapping := mc.collectionState.mappings.data[getTypedKey(input{Named: Named{Name: "parent"}})]
		assert.Equal(t, mapping.many == nil, len(keys) <= 1)
		if mapping.many != nil {
			assert.Equal(t, len(mapping.many.data), len(keys))
			for _, key := range keys {
				_, found := mapping.many.data[getTypedKey(Named{Name: key})]
				assert.Equal(t, found, true)
			}
			assert.Equal(t, mapping.many.peak < 1024, len(keys) < 1024)
		}
		mc.mu.RUnlock()
	}
	parents.DeleteObject(GetKey(input{Named: Named{Name: "parent"}}))
	assert.EventuallyEqual(t, func() int { return len(derived.List()) }, 0)
}
