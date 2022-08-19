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
	"istio.io/istio/pkg/util/sets"
)

type entry struct {
	key              string
	dependentTypes   []kind.Kind
	dependentConfigs []ConfigHash
}

func (e *entry) Key() string {
	return e.key
}

func (e *entry) DependentTypes() []kind.Kind {
	return e.dependentTypes
}

func (e *entry) DependentConfigs() []ConfigHash {
	return e.dependentConfigs
}

func (e *entry) Cacheable() bool {
	return true
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

	c := NewXdsCache()
	cache := c.(*lruCache)

	assert.Equal(t, cache.store.Len(), 0)
	assert.Equal(t, cache.configIndex, map[ConfigHash]sets.Set{})
	assert.Equal(t, cache.typesIndex, map[kind.Kind]sets.Set{})

	// adding the entry populates the indexes
	c.Add(&firstEntry, req, res)

	assert.Equal(t, cache.store.Len(), 1)
	assert.Equal(t, len(cache.configIndex), 2)
	assert.Equal(t, len(cache.typesIndex), 2)
	assert.Equal(t, cache.configIndex, map[ConfigHash]sets.Set{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.key),
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(firstEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[kind.Kind]sets.Set{
		kind.Service:         sets.New(firstEntry.key),
		kind.DestinationRule: sets.New(firstEntry.key),
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
	c.Add(&secondEntry, req, res)

	assert.Equal(t, cache.store.Len(), 2)
	assert.Equal(t, len(cache.configIndex), 3)
	assert.Equal(t, len(cache.typesIndex), 3)
	assert.Equal(t, cache.configIndex, map[ConfigHash]sets.Set{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.key, secondEntry.key),
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(firstEntry.key),
		ConfigKey{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}.HashCode():     sets.New(secondEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[kind.Kind]sets.Set{
		kind.Service:         sets.New(firstEntry.key, secondEntry.key),
		kind.DestinationRule: sets.New(firstEntry.key),
		kind.EnvoyFilter:     sets.New(secondEntry.key),
	})
}

func TestCleanIndexesOnAddExistant(t *testing.T) {
	test.SetForTest(t, &features.XDSCacheMaxSize, 1)
	zeroTime := time.Time{}
	res := &discovery.Resource{Name: "test"}
	req := &PushRequest{Start: zeroTime.Add(time.Duration(1))}
	firstEntry := entry{
		key:              "key",
		dependentTypes:   []kind.Kind{kind.Service},
		dependentConfigs: []ConfigHash{ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode()},
	}

	c := NewXdsCache()
	cache := c.(*lruCache)

	assert.Equal(t, cache.store.Len(), 0)
	assert.Equal(t, cache.configIndex, map[ConfigHash]sets.Set{})
	assert.Equal(t, cache.typesIndex, map[kind.Kind]sets.Set{})

	// adding the entry populates the indexes
	c.Add(&firstEntry, req, res)

	assert.Equal(t, cache.store.Len(), 1)
	assert.Equal(t, len(cache.configIndex), 1)
	assert.Equal(t, len(cache.typesIndex), 1)
	assert.Equal(t, cache.configIndex, map[ConfigHash]sets.Set{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(firstEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[kind.Kind]sets.Set{kind.Service: sets.New(firstEntry.key)})

	// second entry has the same key but different dependencies
	secondEntry := entry{
		key:              "key",
		dependentTypes:   []kind.Kind{kind.DestinationRule},
		dependentConfigs: []ConfigHash{ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode()},
	}

	// after adding an entry with the same key, previous indexes are correctly cleaned
	c.Add(&secondEntry, req, res)

	assert.Equal(t, cache.store.Len(), 1)
	assert.Equal(t, len(cache.configIndex), 1)
	assert.Equal(t, len(cache.typesIndex), 1)
	assert.Equal(t, cache.configIndex, map[ConfigHash]sets.Set{
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(secondEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[kind.Kind]sets.Set{kind.DestinationRule: sets.New(secondEntry.key)})
}

func TestCleanIndexesOnEvict(t *testing.T) {
	test.SetForTest(t, &features.XDSCacheMaxSize, 1)
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

	c := NewXdsCache()
	cache := c.(*lruCache)

	assert.Equal(t, cache.store.Len(), 0)
	assert.Equal(t, cache.configIndex, map[ConfigHash]sets.Set{})
	assert.Equal(t, cache.typesIndex, map[kind.Kind]sets.Set{})

	// adding the entry populates the indexes
	c.Add(&firstEntry, req, res)

	assert.Equal(t, cache.store.Len(), 1)
	assert.Equal(t, len(cache.configIndex), 2)
	assert.Equal(t, len(cache.typesIndex), 2)
	assert.Equal(t, cache.configIndex, map[ConfigHash]sets.Set{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.key),
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(firstEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[kind.Kind]sets.Set{
		kind.Service:         sets.New(firstEntry.key),
		kind.DestinationRule: sets.New(firstEntry.key),
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
	c.Add(&secondEntry, req, res)

	assert.Equal(t, cache.store.Len(), 1)
	assert.Equal(t, len(cache.configIndex), 2)
	assert.Equal(t, len(cache.typesIndex), 2)
	assert.Equal(t, cache.configIndex, map[ConfigHash]sets.Set{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():     sets.New(secondEntry.key),
		ConfigKey{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(secondEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[kind.Kind]sets.Set{
		kind.Service:     sets.New(secondEntry.key),
		kind.EnvoyFilter: sets.New(secondEntry.key),
	})
}

func TestCleanIndexesOnCacheClear(t *testing.T) {
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

	c := NewXdsCache()
	cache := c.(*lruCache)

	c.Add(&firstEntry, req1, res)
	c.Add(&secondEntry, req2, res)

	// indexes populated
	assert.Equal(t, cache.store.Len(), 2)
	assert.Equal(t, len(cache.configIndex), 5)
	assert.Equal(t, len(cache.typesIndex), 5)
	assert.Equal(t, cache.configIndex, map[ConfigHash]sets.Set{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.key, secondEntry.key),
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(firstEntry.key),
		ConfigKey{Kind: kind.Gateway, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.key),
		ConfigKey{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}.HashCode():     sets.New(secondEntry.key),
		ConfigKey{Kind: kind.WasmPlugin, Name: "name", Namespace: "namespace"}.HashCode():      sets.New(secondEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[kind.Kind]sets.Set{
		kind.Service:         sets.New(firstEntry.key, secondEntry.key),
		kind.DestinationRule: sets.New(firstEntry.key),
		kind.Gateway:         sets.New(firstEntry.key),
		kind.EnvoyFilter:     sets.New(secondEntry.key),
		kind.WasmPlugin:      sets.New(secondEntry.key),
	})

	cache.Clear(map[ConfigKey]struct{}{})

	// no change on empty clear
	assert.Equal(t, cache.store.Len(), 2)
	assert.Equal(t, len(cache.configIndex), 5)
	assert.Equal(t, len(cache.typesIndex), 5)
	assert.Equal(t, cache.configIndex, map[ConfigHash]sets.Set{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.key, secondEntry.key),
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(firstEntry.key),
		ConfigKey{Kind: kind.Gateway, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.key),
		ConfigKey{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}.HashCode():     sets.New(secondEntry.key),
		ConfigKey{Kind: kind.WasmPlugin, Name: "name", Namespace: "namespace"}.HashCode():      sets.New(secondEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[kind.Kind]sets.Set{
		kind.Service:         sets.New(firstEntry.key, secondEntry.key),
		kind.DestinationRule: sets.New(firstEntry.key),
		kind.Gateway:         sets.New(firstEntry.key),
		kind.EnvoyFilter:     sets.New(secondEntry.key),
		kind.WasmPlugin:      sets.New(secondEntry.key),
	})

	// clear only DestinationRule dependencies, should clear all firstEntry references
	cache.Clear(map[ConfigKey]struct{}{{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}: {}})

	assert.Equal(t, cache.store.Len(), 1)
	assert.Equal(t, len(cache.configIndex), 3)
	assert.Equal(t, len(cache.typesIndex), 3)
	assert.Equal(t, cache.configIndex, map[ConfigHash]sets.Set{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():     sets.New(secondEntry.key),
		ConfigKey{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(secondEntry.key),
		ConfigKey{Kind: kind.WasmPlugin, Name: "name", Namespace: "namespace"}.HashCode():  sets.New(secondEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[kind.Kind]sets.Set{
		kind.Service:     sets.New(secondEntry.key),
		kind.EnvoyFilter: sets.New(secondEntry.key),
		kind.WasmPlugin:  sets.New(secondEntry.key),
	})

	// add firstEntry again
	c.Add(&firstEntry, &PushRequest{Start: zeroTime.Add(time.Duration(3))}, res)

	assert.Equal(t, cache.store.Len(), 2)
	assert.Equal(t, len(cache.configIndex), 5)
	assert.Equal(t, len(cache.typesIndex), 5)
	assert.Equal(t, cache.configIndex, map[ConfigHash]sets.Set{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.key, secondEntry.key),
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(firstEntry.key),
		ConfigKey{Kind: kind.Gateway, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.key),
		ConfigKey{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}.HashCode():     sets.New(secondEntry.key),
		ConfigKey{Kind: kind.WasmPlugin, Name: "name", Namespace: "namespace"}.HashCode():      sets.New(secondEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[kind.Kind]sets.Set{
		kind.Service:         sets.New(firstEntry.key, secondEntry.key),
		kind.DestinationRule: sets.New(firstEntry.key),
		kind.Gateway:         sets.New(firstEntry.key),
		kind.EnvoyFilter:     sets.New(secondEntry.key),
		kind.WasmPlugin:      sets.New(secondEntry.key),
	})

	// clear only EnvoyFilter dependencies, should clear all secondEntry references
	cache.Clear(map[ConfigKey]struct{}{{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}: {}})

	assert.Equal(t, cache.store.Len(), 1)
	assert.Equal(t, len(cache.configIndex), 3)
	assert.Equal(t, len(cache.typesIndex), 3)
	assert.Equal(t, cache.configIndex, map[ConfigHash]sets.Set{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.key),
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(firstEntry.key),
		ConfigKey{Kind: kind.Gateway, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[kind.Kind]sets.Set{
		kind.Service:         sets.New(firstEntry.key),
		kind.DestinationRule: sets.New(firstEntry.key),
		kind.Gateway:         sets.New(firstEntry.key),
	})

	// add secondEntry again
	c.Add(&secondEntry, &PushRequest{Start: zeroTime.Add(time.Duration(4))}, res)

	assert.Equal(t, cache.store.Len(), 2)
	assert.Equal(t, len(cache.configIndex), 5)
	assert.Equal(t, len(cache.typesIndex), 5)
	assert.Equal(t, cache.configIndex, map[ConfigHash]sets.Set{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.key, secondEntry.key),
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(firstEntry.key),
		ConfigKey{Kind: kind.Gateway, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.key),
		ConfigKey{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}.HashCode():     sets.New(secondEntry.key),
		ConfigKey{Kind: kind.WasmPlugin, Name: "name", Namespace: "namespace"}.HashCode():      sets.New(secondEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[kind.Kind]sets.Set{
		kind.Service:         sets.New(firstEntry.key, secondEntry.key),
		kind.DestinationRule: sets.New(firstEntry.key),
		kind.Gateway:         sets.New(firstEntry.key),
		kind.EnvoyFilter:     sets.New(secondEntry.key),
		kind.WasmPlugin:      sets.New(secondEntry.key),
	})

	// clear only Service dependencies, should clear both firstEntry and secondEntry references
	cache.Clear(map[ConfigKey]struct{}{{Kind: kind.Service, Name: "name", Namespace: "namespace"}: {}})

	assert.Equal(t, len(cache.configIndex), 0)
	assert.Equal(t, len(cache.typesIndex), 0)
	assert.Equal(t, cache.store.Len(), 0)
	assert.Equal(t, cache.configIndex, map[ConfigHash]sets.Set{})
	assert.Equal(t, cache.typesIndex, map[kind.Kind]sets.Set{})
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

	c := NewXdsCache()
	cache := c.(*lruCache)

	c.Add(&firstEntry, req1, res)
	c.Add(&secondEntry, req2, res)

	// indexes populated
	assert.Equal(t, len(cache.configIndex), 5)
	assert.Equal(t, len(cache.typesIndex), 5)
	assert.Equal(t, cache.configIndex, map[ConfigHash]sets.Set{
		ConfigKey{Kind: kind.Service, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.key, secondEntry.key),
		ConfigKey{Kind: kind.DestinationRule, Name: "name", Namespace: "namespace"}.HashCode(): sets.New(firstEntry.key),
		ConfigKey{Kind: kind.Gateway, Name: "name", Namespace: "namespace"}.HashCode():         sets.New(firstEntry.key),
		ConfigKey{Kind: kind.EnvoyFilter, Name: "name", Namespace: "namespace"}.HashCode():     sets.New(secondEntry.key),
		ConfigKey{Kind: kind.WasmPlugin, Name: "name", Namespace: "namespace"}.HashCode():      sets.New(secondEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[kind.Kind]sets.Set{
		kind.Service:         sets.New(firstEntry.key, secondEntry.key),
		kind.DestinationRule: sets.New(firstEntry.key),
		kind.Gateway:         sets.New(firstEntry.key),
		kind.EnvoyFilter:     sets.New(secondEntry.key),
		kind.WasmPlugin:      sets.New(secondEntry.key),
	})

	cache.ClearAll()

	// no change on empty clear
	assert.Equal(t, len(cache.configIndex), 0)
	assert.Equal(t, len(cache.typesIndex), 0)
	assert.Equal(t, cache.store.Len(), 0)
}
