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
	"testing"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/hash"
	"istio.io/istio/pkg/util/sets"
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
	test.SetForTest(t, &features.XDSCacheMaxSize, 10)
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
	defer c.Close()

	cache := c.(*lruCache[uint64])

	assert.Equal(t, cache.store.Size(), 0)
	assert.Equal(t, cache.indexLength(), 0)

	// adding the entry populates the indexes
	c.Add(firstEntry.Key(), firstEntry, req, res)

	assert.Equal(t, cache.store.Size(), 1)
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

	assert.Equal(t, cache.store.Size(), 2)
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
	defer c.Close()
	cache := c.(*lruCache[uint64])

	assert.Equal(t, cache.store.Size(), 0)
	assert.Equal(t, cache.indexLength(), 0)

	// adding the entry populates the indexes
	c.Add(firstEntry.Key(), firstEntry, req, res)

	assert.Equal(t, cache.store.Size(), 1)
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

	assert.Equal(t, cache.store.Size(), 1)

	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, cache.indexLength(), 1)

	assert.Equal(t, cache.configIndexSnapshot(), map[ConfigHash]sets.Set[uint64]{
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(secondEntry.Key()),
	})
}

func TestCleanIndexesOnEvict(t *testing.T) {
	test.SetForTest(t, &features.XDSCacheMaxSize, 10)
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
	defer c.Close()
	cache := c.(*lruCache[uint64])

	assert.Equal(t, cache.store.Size(), 0)
	assert.Equal(t, cache.indexLength(), 0)

	// adding the entry populates the indexes
	c.Add(firstEntry.Key(), firstEntry, req, res)

	assert.Equal(t, cache.store.Size(), 1)
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
	assert.Equal(t, cache.store.Size(), 2)

	// simulate cache eviction, will call deletion callback
	cache.store.Delete(firstEntry.Key())
	assert.Equal(t, cache.store.Size(), 1)

	time.Sleep(1 * time.Millisecond)

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
	defer c.Close()
	cache := c.(*lruCache[uint64])

	c.Add(firstEntry.Key(), firstEntry, req1, res)
	c.Add(secondEntry.Key(), secondEntry, req2, res)

	// indexes populated
	assert.Equal(t, cache.store.Size(), 2)
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
	assert.Equal(t, cache.store.Size(), 2)
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

	assert.Equal(t, cache.store.Size(), 1)

	time.Sleep(1 * time.Millisecond)
	assert.Equal(t, cache.indexLength(), 3)

	assert.Equal(t, cache.configIndexSnapshot(), map[ConfigHash]sets.Set[uint64]{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():     sets.New(secondEntry.Key()),
		ConfigKey{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(secondEntry.Key()),
		ConfigKey{Kind: kind.WasmPlugin, Name: "name", Namespace: "namespace"}.HashCode():  sets.New(secondEntry.Key()),
	})

	// add firstEntry again
	c.Add(firstEntry.Key(), firstEntry, &PushRequest{Start: zeroTime.Add(time.Duration(3))}, res)

	assert.Equal(t, cache.store.Size(), 2)
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

	assert.Equal(t, cache.store.Size(), 1)

	time.Sleep(1 * time.Millisecond)

	assert.Equal(t, cache.indexLength(), 3)

	assert.Equal(t, cache.configIndexSnapshot(), map[ConfigHash]sets.Set[uint64]{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.Key()),
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(firstEntry.Key()),
		ConfigKey{Kind: kind.Gateway, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.Key()),
	})

	// add secondEntry again
	c.Add(secondEntry.Key(), secondEntry, &PushRequest{Start: zeroTime.Add(time.Duration(4))}, res)

	assert.Equal(t, cache.store.Size(), 2)
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

	time.Sleep(1 * time.Millisecond)

	assert.Equal(t, cache.indexLength(), 0)

	assert.Equal(t, cache.store.Size(), 0)
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
	defer c.Close()
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
	assert.Equal(t, cache.store.Size(), 0)
}
