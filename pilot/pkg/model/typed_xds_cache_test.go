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

package model

import (
	"reflect"
	"testing"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/hash"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/tests/util/leak"
)

type entry struct {
	key              string
	dependentTypes   []kind.Kind
	dependentConfigs []ConfigHash
}

func (e entry) Key() uint64 {
	h := hash.New()
	h.WriteString(e.key)
	return h.Sum64()
}

func (e entry) DependentConfigs() []ConfigHash {
	return e.dependentConfigs
}

func TestAddTwoEntries(t *testing.T) {
	test.SetForTest(t, &features.XDSCacheMaxSize, 2)
	zeroTime := time.Time{}
	res := &discovery.Resource{Name: "test"}
	req := &PushRequest{Start: zeroTime.Add(time.Duration(1))}
	firstEntry := entry{
		key:            "key1",
		dependentTypes: []kind.Kind{kind.Service, kind.DestinationRule},
		dependentConfigs: []ConfigHash{
			ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode(),
			ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(),
		},
	}

	c := newTypedXdsCache[uint64]()

	cache := c.(*lruCache[uint64])

	assert.Equal(t, cache.store.Len(), 0)
	assert.Equal(t, cache.indexLength(), 0)

	// adding the entry populates the indexes
	c.Add(firstEntry.Key(), firstEntry, req, res)

	assert.Equal(t, cache.store.Len(), 1)
	assert.Equal(t, cache.indexLength(), 2)
	assert.Equal(t, cache.configIndexSnapshot(), map[ConfigHash]sets.Set[uint64]{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.Key()),
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(firstEntry.Key()),
	})

	// second entry has different key and dependencies
	secondEntry := entry{
		key:            "key2",
		dependentTypes: []kind.Kind{kind.Service, kind.EnvoyFilter},
		dependentConfigs: []ConfigHash{
			ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode(),
			ConfigKey{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}.HashCode(),
		},
	}

	// after adding an index with a different key, indexes are populated with both dependencies
	c.Add(secondEntry.Key(), secondEntry, req, res)

	assert.Equal(t, cache.store.Len(), 2)
	assert.Equal(t, cache.indexLength(), 3)
	assert.Equal(t, cache.configIndexSnapshot(), map[ConfigHash]sets.Set[uint64]{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.Key(), secondEntry.Key()),
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(firstEntry.Key()),
		ConfigKey{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}.HashCode():     sets.New(secondEntry.Key()),
	})
}

func TestCleanIndexesOnAddExistant(t *testing.T) {
	test.SetForTest(t, &features.XDSCacheIndexClearInterval, 5*time.Millisecond)
	zeroTime := time.Time{}
	res := &discovery.Resource{Name: "test"}
	req := &PushRequest{Start: zeroTime.Add(time.Duration(1))}
	firstEntry := entry{
		key:              "key",
		dependentTypes:   []kind.Kind{kind.Service},
		dependentConfigs: []ConfigHash{ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode()},
	}

	c := newTypedXdsCache[uint64]()
	cache := c.(*lruCache[uint64])

	assert.Equal(t, cache.store.Len(), 0)
	assert.Equal(t, cache.indexLength(), 0)

	// adding the entry populates the indexes
	c.Add(firstEntry.Key(), firstEntry, req, res)

	assert.Equal(t, cache.store.Len(), 1)
	assert.Equal(t, cache.indexLength(), 1)
	assert.Equal(t, cache.configIndexSnapshot(), map[ConfigHash]sets.Set[uint64]{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(firstEntry.Key()),
	})

	// second entry has the same key but different dependencies
	secondEntry := entry{
		key:              "key",
		dependentTypes:   []kind.Kind{kind.DestinationRule},
		dependentConfigs: []ConfigHash{ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode()},
	}
	req = &PushRequest{Start: zeroTime.Add(time.Duration(2))}
	// after adding an entry with the same key, previous indexes are correctly cleaned
	c.Add(secondEntry.Key(), secondEntry, req, res)

	assert.Equal(t, cache.store.Len(), 1)

	// Flush the cache and validate the index is cleaned.
	cache.Flush()
	assert.Equal(t, cache.indexLength(), 1)

	assert.Equal(t, cache.configIndexSnapshot(), map[ConfigHash]sets.Set[uint64]{
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(secondEntry.Key()),
	})
}

func TestCleanIndexesOnEvict(t *testing.T) {
	test.SetForTest(t, &features.XDSCacheMaxSize, 1)
	test.SetForTest(t, &features.XDSCacheIndexClearInterval, 5*time.Millisecond)
	zeroTime := time.Time{}
	res := &discovery.Resource{Name: "test"}
	req := &PushRequest{Start: zeroTime.Add(time.Duration(1))}
	firstEntry := entry{
		key:            "key1",
		dependentTypes: []kind.Kind{kind.Service, kind.DestinationRule},
		dependentConfigs: []ConfigHash{
			ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode(),
			ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(),
		},
	}

	c := newTypedXdsCache[uint64]()
	cache := c.(*lruCache[uint64])

	assert.Equal(t, cache.store.Len(), 0)
	assert.Equal(t, cache.indexLength(), 0)

	// adding the entry populates the indexes
	c.Add(firstEntry.Key(), firstEntry, req, res)

	assert.Equal(t, cache.store.Len(), 1)
	assert.Equal(t, cache.indexLength(), 2)
	assert.Equal(t, cache.configIndexSnapshot(), map[ConfigHash]sets.Set[uint64]{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.Key()),
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(firstEntry.Key()),
	})

	// second entry has different key and dependencies
	secondEntry := entry{
		key:            "key2",
		dependentTypes: []kind.Kind{kind.Service, kind.EnvoyFilter},
		dependentConfigs: []ConfigHash{
			ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode(),
			ConfigKey{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}.HashCode(),
		},
	}

	// after adding an index with a different key, first key is evicted, idexes should contain only secondEntry
	c.Add(secondEntry.Key(), secondEntry, req, res)

	assert.Equal(t, cache.store.Len(), 1)

	// Flush the cache and validate the index is cleaned.
	cache.Flush()
	assert.Equal(t, cache.indexLength(), 2)

	assert.Equal(t, cache.configIndexSnapshot(), map[ConfigHash]sets.Set[uint64]{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():     sets.New(secondEntry.Key()),
		ConfigKey{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(secondEntry.Key()),
	})
}

func TestCleanIndexesOnCacheClear(t *testing.T) {
	test.SetForTest(t, &features.XDSCacheMaxSize, 10)
	test.SetForTest(t, &features.XDSCacheIndexClearInterval, 5*time.Millisecond)
	zeroTime := time.Time{}
	res := &discovery.Resource{Name: "test"}
	req1 := &PushRequest{Start: zeroTime.Add(time.Duration(1))}
	req2 := &PushRequest{Start: zeroTime.Add(time.Duration(2))}
	firstEntry := entry{
		key:            "key1",
		dependentTypes: []kind.Kind{kind.Service, kind.DestinationRule, kind.Gateway},
		dependentConfigs: []ConfigHash{
			ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode(),
			ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(),
			ConfigKey{Kind: kind.Gateway, Name: "name", Namespace: "namespace"}.HashCode(),
		},
	}

	// second entry has different key and dependencies
	secondEntry := entry{
		key:            "key2",
		dependentTypes: []kind.Kind{kind.Service, kind.EnvoyFilter, kind.WasmPlugin},
		dependentConfigs: []ConfigHash{
			ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode(),
			ConfigKey{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}.HashCode(),
			ConfigKey{Kind: kind.WasmPlugin, Name: "name", Namespace: "namespace"}.HashCode(),
		},
	}

	c := newTypedXdsCache[uint64]()
	cache := c.(*lruCache[uint64])

	c.Add(firstEntry.Key(), firstEntry, req1, res)
	c.Add(secondEntry.Key(), secondEntry, req2, res)

	// indexes populated
	assert.Equal(t, cache.store.Len(), 2)
	assert.Equal(t, cache.indexLength(), 5)
	assert.Equal(t, cache.configIndexSnapshot(), map[ConfigHash]sets.Set[uint64]{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.Key(), secondEntry.Key()),
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(firstEntry.Key()),
		ConfigKey{Kind: kind.Gateway, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.Key()),
		ConfigKey{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}.HashCode():     sets.New(secondEntry.Key()),
		ConfigKey{Kind: kind.WasmPlugin, Name: "name", Namespace: "namespace"}.HashCode():      sets.New(secondEntry.Key()),
	})

	cache.Clear(sets.Set[ConfigKey]{})

	// no change on empty clear
	assert.Equal(t, cache.store.Len(), 2)
	assert.Equal(t, cache.indexLength(), 5)
	assert.Equal(t, cache.configIndexSnapshot(), map[ConfigHash]sets.Set[uint64]{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.Key(), secondEntry.Key()),
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(firstEntry.Key()),
		ConfigKey{Kind: kind.Gateway, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.Key()),
		ConfigKey{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}.HashCode():     sets.New(secondEntry.Key()),
		ConfigKey{Kind: kind.WasmPlugin, Name: "name", Namespace: "namespace"}.HashCode():      sets.New(secondEntry.Key()),
	})

	// clear only DestinationRule dependencies, should clear all firstEntry references
	cache.Clear(sets.Set[ConfigKey]{{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}: {}})

	assert.Equal(t, cache.store.Len(), 1)

	// Flush the cache and validate the index is cleaned.
	cache.Flush()
	assert.Equal(t, cache.indexLength(), 3)

	assert.Equal(t, cache.configIndexSnapshot(), map[ConfigHash]sets.Set[uint64]{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():     sets.New(secondEntry.Key()),
		ConfigKey{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(secondEntry.Key()),
		ConfigKey{Kind: kind.WasmPlugin, Name: "name", Namespace: "namespace"}.HashCode():  sets.New(secondEntry.Key()),
	})

	// add firstEntry again
	c.Add(firstEntry.Key(), firstEntry, &PushRequest{Start: zeroTime.Add(time.Duration(3))}, res)

	assert.Equal(t, cache.store.Len(), 2)

	// Flush the cache and validate the index is cleaned.
	cache.Flush()
	assert.Equal(t, cache.indexLength(), 5)

	assert.Equal(t, cache.configIndexSnapshot(), map[ConfigHash]sets.Set[uint64]{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.Key(), secondEntry.Key()),
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(firstEntry.Key()),
		ConfigKey{Kind: kind.Gateway, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.Key()),
		ConfigKey{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}.HashCode():     sets.New(secondEntry.Key()),
		ConfigKey{Kind: kind.WasmPlugin, Name: "name", Namespace: "namespace"}.HashCode():      sets.New(secondEntry.Key()),
	})

	// clear only EnvoyFilter dependencies, should clear all secondEntry references
	cache.Clear(sets.Set[ConfigKey]{{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}: {}})

	assert.Equal(t, cache.store.Len(), 1)

	// Flush the cache and validate the index is cleaned.
	cache.Flush()
	assert.Equal(t, cache.indexLength(), 3)

	assert.Equal(t, cache.configIndexSnapshot(), map[ConfigHash]sets.Set[uint64]{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.Key()),
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(firstEntry.Key()),
		ConfigKey{Kind: kind.Gateway, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.Key()),
	})

	// add secondEntry again
	c.Add(secondEntry.Key(), secondEntry, &PushRequest{Start: zeroTime.Add(time.Duration(4))}, res)

	assert.Equal(t, cache.store.Len(), 2)

	// Flush the cache and validate the index is cleaned.
	cache.Flush()
	assert.Equal(t, cache.indexLength(), 5)

	assert.Equal(t, cache.configIndexSnapshot(), map[ConfigHash]sets.Set[uint64]{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.Key(), secondEntry.Key()),
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(firstEntry.Key()),
		ConfigKey{Kind: kind.Gateway, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.Key()),
		ConfigKey{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}.HashCode():     sets.New(secondEntry.Key()),
		ConfigKey{Kind: kind.WasmPlugin, Name: "name", Namespace: "namespace"}.HashCode():      sets.New(secondEntry.Key()),
	})

	// clear only Service dependencies, should clear both firstEntry and secondEntry references
	cache.Clear(sets.Set[ConfigKey]{{Kind: kind.Service, Name: "name", Namespace: "namespace"}: {}})

	// Flush the cache and validate the index is cleaned.
	cache.Flush()
	assert.Equal(t, cache.indexLength(), 0)

	assert.Equal(t, cache.store.Len(), 0)
}

func TestCacheClearAll(t *testing.T) {
	test.SetForTest(t, &features.XDSCacheMaxSize, 10)
	zeroTime := time.Time{}
	res := &discovery.Resource{Name: "test"}
	req1 := &PushRequest{Start: zeroTime.Add(time.Duration(1))}
	req2 := &PushRequest{Start: zeroTime.Add(time.Duration(2))}
	firstEntry := entry{
		key:            "key1",
		dependentTypes: []kind.Kind{kind.Service, kind.DestinationRule, kind.Gateway},
		dependentConfigs: []ConfigHash{
			ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode(),
			ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(),
			ConfigKey{Kind: kind.Gateway, Name: "name", Namespace: "namespace"}.HashCode(),
		},
	}

	// second entry has different key and dependencies
	secondEntry := entry{
		key:            "key2",
		dependentTypes: []kind.Kind{kind.Service, kind.EnvoyFilter, kind.WasmPlugin},
		dependentConfigs: []ConfigHash{
			ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode(),
			ConfigKey{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}.HashCode(),
			ConfigKey{Kind: kind.WasmPlugin, Name: "name", Namespace: "namespace"}.HashCode(),
		},
	}

	c := newTypedXdsCache[uint64]()
	cache := c.(*lruCache[uint64])

	c.Add(firstEntry.Key(), firstEntry, req1, res)
	c.Add(secondEntry.Key(), secondEntry, req2, res)

	// indexes populated
	assert.Equal(t, cache.indexLength(), 5)
	assert.Equal(t, cache.configIndexSnapshot(), map[ConfigHash]sets.Set[uint64]{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.Key(), secondEntry.Key()),
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(firstEntry.Key()),
		ConfigKey{Kind: kind.Gateway, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.Key()),
		ConfigKey{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}.HashCode():     sets.New(secondEntry.Key()),
		ConfigKey{Kind: kind.WasmPlugin, Name: "name", Namespace: "namespace"}.HashCode():      sets.New(secondEntry.Key()),
	})

	cache.ClearAll()

	// no change on empty clear
	assert.Equal(t, cache.indexLength(), 0)
	assert.Equal(t, cache.store.Len(), 0)
}

func TestEvictQueueMemoryLeak(t *testing.T) {
	testEvictQueueMemoryLeak(t, "Flush")
	testEvictQueueMemoryLeak(t, "ClearAll")
}

func testEvictQueueMemoryLeak(t *testing.T, f string) {
	entry1 := entry{
		key:            "key",
		dependentTypes: []kind.Kind{kind.Service},
		dependentConfigs: []ConfigHash{
			ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode(),
		},
	}
	entry2 := entry{
		key:            "key", // use the same key so that the old one will be pushed to the evictQueue
		dependentTypes: []kind.Kind{kind.Service, kind.DestinationRule},
		dependentConfigs: []ConfigHash{
			ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode(),
			ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(),
		},
	}

	zeroTime := time.Time{}
	push1 := &PushRequest{Start: zeroTime.Add(time.Duration(1))}
	push2 := &PushRequest{Start: zeroTime.Add(time.Duration(2))}

	value := &discovery.Resource{Name: "test"}

	// build xds cache
	c := newTypedXdsCache[uint64]()
	c.Add(entry1.Key(), entry1, push1, value)
	c.Add(entry2.Key(), entry2, push2, value)

	// When Flush (or ClearAll) is called, the length of `cache.evictQueue` becomes 0, and we
	// cannot use it to check whether the elements referenced by the underlying array are released.
	// Since shallow copy shares the underlying array, we can use it to check.
	cache := c.(*lruCache[uint64])
	evictQueue := cache.evictQueue
	for _, item := range evictQueue {
		leak.MustGarbageCollect(t, &item)
	}

	if f == "Flush" {
		cache.Flush()
	} else {
		cache.ClearAll()
	}

	// Checks that the elements referenced by the underlying array have been released.
	var empty evictKeyConfigs[uint64]
	for _, item := range evictQueue {
		if !reflect.DeepEqual(item, empty) {
			t.Fatalf("test %s func, expected empty value, but got %+v", f, item)
		}
	}
}
