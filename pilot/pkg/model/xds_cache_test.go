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
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

type entry struct {
	key              string
	dependentTypes   []config.GroupVersionKind
	dependentConfigs []ConfigKey
}

func (e *entry) Key() string {
	return e.key
}

func (e *entry) DependentTypes() []config.GroupVersionKind {
	return e.dependentTypes
}

func (e *entry) DependentConfigs() []ConfigKey {
	return e.dependentConfigs
}

func (e *entry) Cacheable() bool {
	return true
}

func TestAddTwoEntries(t *testing.T) {
	test.SetIntForTest(t, &features.XDSCacheMaxSize, 2)
	zeroTime := time.Time{}
	res := &discovery.Resource{Name: "test"}
	req := &PushRequest{Start: zeroTime.Add(time.Duration(1))}
	firstEntry := entry{
		key:            "key1",
		dependentTypes: []config.GroupVersionKind{gvk.Service, gvk.DestinationRule},
		dependentConfigs: []ConfigKey{
			{Kind: gvk.Service, Name: "name", Namespace: "namespace"},
			{Kind: gvk.DestinationRule, Name: "name", Namespace: "namespace"},
		},
	}

	c := NewXdsCache()
	cache := c.(*lruCache)

	assert.Equal(t, cache.store.Len(), 0)
	assert.Equal(t, cache.configIndex, map[ConfigKey]sets.Set{})
	assert.Equal(t, cache.typesIndex, map[config.GroupVersionKind]sets.Set{})

	// adding the entry populates the indexes
	c.Add(&firstEntry, req, res)

	assert.Equal(t, cache.store.Len(), 1)
	assert.Equal(t, len(cache.configIndex), 2)
	assert.Equal(t, len(cache.typesIndex), 2)
	assert.Equal(t, cache.configIndex, map[ConfigKey]sets.Set{
		{Kind: gvk.Service, Name: "name", Namespace: "namespace"}:         sets.New(firstEntry.key),
		{Kind: gvk.DestinationRule, Name: "name", Namespace: "namespace"}: sets.New(firstEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[config.GroupVersionKind]sets.Set{
		gvk.Service:         sets.New(firstEntry.key),
		gvk.DestinationRule: sets.New(firstEntry.key),
	})

	// second entry has different key and dependencies
	secondEntry := entry{
		key:            "key2",
		dependentTypes: []config.GroupVersionKind{gvk.Service, gvk.EnvoyFilter},
		dependentConfigs: []ConfigKey{
			{Kind: gvk.Service, Name: "name", Namespace: "namespace"},
			{Kind: gvk.EnvoyFilter, Name: "name", Namespace: "namespace"},
		},
	}

	// after adding an index with a different key, indexes are populated with both dependencies
	c.Add(&secondEntry, req, res)

	assert.Equal(t, cache.store.Len(), 2)
	assert.Equal(t, len(cache.configIndex), 3)
	assert.Equal(t, len(cache.typesIndex), 3)
	assert.Equal(t, cache.configIndex, map[ConfigKey]sets.Set{
		{Kind: gvk.Service, Name: "name", Namespace: "namespace"}:         sets.New(firstEntry.key, secondEntry.key),
		{Kind: gvk.DestinationRule, Name: "name", Namespace: "namespace"}: sets.New(firstEntry.key),
		{Kind: gvk.EnvoyFilter, Name: "name", Namespace: "namespace"}:     sets.New(secondEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[config.GroupVersionKind]sets.Set{
		gvk.Service:         sets.New(firstEntry.key, secondEntry.key),
		gvk.DestinationRule: sets.New(firstEntry.key),
		gvk.EnvoyFilter:     sets.New(secondEntry.key),
	})
}

func TestCleanIndexesOnAddExistant(t *testing.T) {
	test.SetIntForTest(t, &features.XDSCacheMaxSize, 1)
	zeroTime := time.Time{}
	res := &discovery.Resource{Name: "test"}
	req := &PushRequest{Start: zeroTime.Add(time.Duration(1))}
	firstEntry := entry{
		key:              "key",
		dependentTypes:   []config.GroupVersionKind{gvk.Service},
		dependentConfigs: []ConfigKey{{Kind: gvk.Service, Name: "name", Namespace: "namespace"}},
	}

	c := NewXdsCache()
	cache := c.(*lruCache)

	assert.Equal(t, cache.store.Len(), 0)
	assert.Equal(t, cache.configIndex, map[ConfigKey]sets.Set{})
	assert.Equal(t, cache.typesIndex, map[config.GroupVersionKind]sets.Set{})

	// adding the entry populates the indexes
	c.Add(&firstEntry, req, res)

	assert.Equal(t, cache.store.Len(), 1)
	assert.Equal(t, len(cache.configIndex), 1)
	assert.Equal(t, len(cache.typesIndex), 1)
	assert.Equal(t, cache.configIndex, map[ConfigKey]sets.Set{{Kind: gvk.Service, Name: "name", Namespace: "namespace"}: sets.New(firstEntry.key)})
	assert.Equal(t, cache.typesIndex, map[config.GroupVersionKind]sets.Set{gvk.Service: sets.New(firstEntry.key)})

	// second entry has the same key but different dependencies
	secondEntry := entry{
		key:              "key",
		dependentTypes:   []config.GroupVersionKind{gvk.DestinationRule},
		dependentConfigs: []ConfigKey{{Kind: gvk.DestinationRule, Name: "name", Namespace: "namespace"}},
	}

	// after adding an entry with the same key, previous indexes are correctly cleaned
	c.Add(&secondEntry, req, res)

	assert.Equal(t, cache.store.Len(), 1)
	assert.Equal(t, len(cache.configIndex), 1)
	assert.Equal(t, len(cache.typesIndex), 1)
	assert.Equal(t, cache.configIndex, map[ConfigKey]sets.Set{{Kind: gvk.DestinationRule, Name: "name", Namespace: "namespace"}: sets.New(secondEntry.key)})
	assert.Equal(t, cache.typesIndex, map[config.GroupVersionKind]sets.Set{gvk.DestinationRule: sets.New(secondEntry.key)})
}

func TestCleanIndexesOnEvict(t *testing.T) {
	test.SetIntForTest(t, &features.XDSCacheMaxSize, 1)
	zeroTime := time.Time{}
	res := &discovery.Resource{Name: "test"}
	req := &PushRequest{Start: zeroTime.Add(time.Duration(1))}
	firstEntry := entry{
		key:            "key1",
		dependentTypes: []config.GroupVersionKind{gvk.Service, gvk.DestinationRule},
		dependentConfigs: []ConfigKey{
			{Kind: gvk.Service, Name: "name", Namespace: "namespace"},
			{Kind: gvk.DestinationRule, Name: "name", Namespace: "namespace"},
		},
	}

	c := NewXdsCache()
	cache := c.(*lruCache)

	assert.Equal(t, cache.store.Len(), 0)
	assert.Equal(t, cache.configIndex, map[ConfigKey]sets.Set{})
	assert.Equal(t, cache.typesIndex, map[config.GroupVersionKind]sets.Set{})

	// adding the entry populates the indexes
	c.Add(&firstEntry, req, res)

	assert.Equal(t, cache.store.Len(), 1)
	assert.Equal(t, len(cache.configIndex), 2)
	assert.Equal(t, len(cache.typesIndex), 2)
	assert.Equal(t, cache.configIndex, map[ConfigKey]sets.Set{
		{Kind: gvk.Service, Name: "name", Namespace: "namespace"}:         sets.New(firstEntry.key),
		{Kind: gvk.DestinationRule, Name: "name", Namespace: "namespace"}: sets.New(firstEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[config.GroupVersionKind]sets.Set{
		gvk.Service:         sets.New(firstEntry.key),
		gvk.DestinationRule: sets.New(firstEntry.key),
	})

	// second entry has different key and dependencies
	secondEntry := entry{
		key:            "key2",
		dependentTypes: []config.GroupVersionKind{gvk.Service, gvk.EnvoyFilter},
		dependentConfigs: []ConfigKey{
			{Kind: gvk.Service, Name: "name", Namespace: "namespace"},
			{Kind: gvk.EnvoyFilter, Name: "name", Namespace: "namespace"},
		},
	}

	// after adding an index with a different key, first key is evicted, idexes should contain only secondEntry
	c.Add(&secondEntry, req, res)

	assert.Equal(t, cache.store.Len(), 1)
	assert.Equal(t, len(cache.configIndex), 2)
	assert.Equal(t, len(cache.typesIndex), 2)
	assert.Equal(t, cache.configIndex, map[ConfigKey]sets.Set{
		{Kind: gvk.Service, Name: "name", Namespace: "namespace"}:     sets.New(secondEntry.key),
		{Kind: gvk.EnvoyFilter, Name: "name", Namespace: "namespace"}: sets.New(secondEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[config.GroupVersionKind]sets.Set{
		gvk.Service:     sets.New(secondEntry.key),
		gvk.EnvoyFilter: sets.New(secondEntry.key),
	})
}

func TestCleanIndexesOnCacheClear(t *testing.T) {
	test.SetIntForTest(t, &features.XDSCacheMaxSize, 10)
	zeroTime := time.Time{}
	res := &discovery.Resource{Name: "test"}
	req1 := &PushRequest{Start: zeroTime.Add(time.Duration(1))}
	req2 := &PushRequest{Start: zeroTime.Add(time.Duration(2))}
	firstEntry := entry{
		key:            "key1",
		dependentTypes: []config.GroupVersionKind{gvk.Service, gvk.DestinationRule, gvk.Gateway},
		dependentConfigs: []ConfigKey{
			{Kind: gvk.Service, Name: "name", Namespace: "namespace"},
			{Kind: gvk.DestinationRule, Name: "name", Namespace: "namespace"},
			{Kind: gvk.Gateway, Name: "name", Namespace: "namespace"},
		},
	}

	// second entry has different key and dependencies
	secondEntry := entry{
		key:            "key2",
		dependentTypes: []config.GroupVersionKind{gvk.Service, gvk.EnvoyFilter, gvk.WasmPlugin},
		dependentConfigs: []ConfigKey{
			{Kind: gvk.Service, Name: "name", Namespace: "namespace"},
			{Kind: gvk.EnvoyFilter, Name: "name", Namespace: "namespace"},
			{Kind: gvk.WasmPlugin, Name: "name", Namespace: "namespace"},
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
	assert.Equal(t, cache.configIndex, map[ConfigKey]sets.Set{
		{Kind: gvk.Service, Name: "name", Namespace: "namespace"}:         sets.New(firstEntry.key, secondEntry.key),
		{Kind: gvk.DestinationRule, Name: "name", Namespace: "namespace"}: sets.New(firstEntry.key),
		{Kind: gvk.Gateway, Name: "name", Namespace: "namespace"}:         sets.New(firstEntry.key),
		{Kind: gvk.EnvoyFilter, Name: "name", Namespace: "namespace"}:     sets.New(secondEntry.key),
		{Kind: gvk.WasmPlugin, Name: "name", Namespace: "namespace"}:      sets.New(secondEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[config.GroupVersionKind]sets.Set{
		gvk.Service:         sets.New(firstEntry.key, secondEntry.key),
		gvk.DestinationRule: sets.New(firstEntry.key),
		gvk.Gateway:         sets.New(firstEntry.key),
		gvk.EnvoyFilter:     sets.New(secondEntry.key),
		gvk.WasmPlugin:      sets.New(secondEntry.key),
	})

	cache.Clear(map[ConfigKey]struct{}{})

	// no change on empty clear
	assert.Equal(t, cache.store.Len(), 2)
	assert.Equal(t, len(cache.configIndex), 5)
	assert.Equal(t, len(cache.typesIndex), 5)
	assert.Equal(t, cache.configIndex, map[ConfigKey]sets.Set{
		{Kind: gvk.Service, Name: "name", Namespace: "namespace"}:         sets.New(firstEntry.key, secondEntry.key),
		{Kind: gvk.DestinationRule, Name: "name", Namespace: "namespace"}: sets.New(firstEntry.key),
		{Kind: gvk.Gateway, Name: "name", Namespace: "namespace"}:         sets.New(firstEntry.key),
		{Kind: gvk.EnvoyFilter, Name: "name", Namespace: "namespace"}:     sets.New(secondEntry.key),
		{Kind: gvk.WasmPlugin, Name: "name", Namespace: "namespace"}:      sets.New(secondEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[config.GroupVersionKind]sets.Set{
		gvk.Service:         sets.New(firstEntry.key, secondEntry.key),
		gvk.DestinationRule: sets.New(firstEntry.key),
		gvk.Gateway:         sets.New(firstEntry.key),
		gvk.EnvoyFilter:     sets.New(secondEntry.key),
		gvk.WasmPlugin:      sets.New(secondEntry.key),
	})

	// clear only DestinationRule dependencies, should clear all firstEntry references
	cache.Clear(map[ConfigKey]struct{}{{Kind: gvk.DestinationRule, Name: "name", Namespace: "namespace"}: {}})

	assert.Equal(t, cache.store.Len(), 1)
	assert.Equal(t, len(cache.configIndex), 3)
	assert.Equal(t, len(cache.typesIndex), 3)
	assert.Equal(t, cache.configIndex, map[ConfigKey]sets.Set{
		{Kind: gvk.Service, Name: "name", Namespace: "namespace"}:     sets.New(secondEntry.key),
		{Kind: gvk.EnvoyFilter, Name: "name", Namespace: "namespace"}: sets.New(secondEntry.key),
		{Kind: gvk.WasmPlugin, Name: "name", Namespace: "namespace"}:  sets.New(secondEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[config.GroupVersionKind]sets.Set{
		gvk.Service:     sets.New(secondEntry.key),
		gvk.EnvoyFilter: sets.New(secondEntry.key),
		gvk.WasmPlugin:  sets.New(secondEntry.key),
	})

	// add firstEntry again
	c.Add(&firstEntry, &PushRequest{Start: zeroTime.Add(time.Duration(3))}, res)

	assert.Equal(t, cache.store.Len(), 2)
	assert.Equal(t, len(cache.configIndex), 5)
	assert.Equal(t, len(cache.typesIndex), 5)
	assert.Equal(t, cache.configIndex, map[ConfigKey]sets.Set{
		{Kind: gvk.Service, Name: "name", Namespace: "namespace"}:         sets.New(firstEntry.key, secondEntry.key),
		{Kind: gvk.DestinationRule, Name: "name", Namespace: "namespace"}: sets.New(firstEntry.key),
		{Kind: gvk.Gateway, Name: "name", Namespace: "namespace"}:         sets.New(firstEntry.key),
		{Kind: gvk.EnvoyFilter, Name: "name", Namespace: "namespace"}:     sets.New(secondEntry.key),
		{Kind: gvk.WasmPlugin, Name: "name", Namespace: "namespace"}:      sets.New(secondEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[config.GroupVersionKind]sets.Set{
		gvk.Service:         sets.New(firstEntry.key, secondEntry.key),
		gvk.DestinationRule: sets.New(firstEntry.key),
		gvk.Gateway:         sets.New(firstEntry.key),
		gvk.EnvoyFilter:     sets.New(secondEntry.key),
		gvk.WasmPlugin:      sets.New(secondEntry.key),
	})

	// clear only EnvoyFilter dependencies, should clear all secondEntry references
	cache.Clear(map[ConfigKey]struct{}{{Kind: gvk.EnvoyFilter, Name: "name", Namespace: "namespace"}: {}})

	assert.Equal(t, cache.store.Len(), 1)
	assert.Equal(t, len(cache.configIndex), 3)
	assert.Equal(t, len(cache.typesIndex), 3)
	assert.Equal(t, cache.configIndex, map[ConfigKey]sets.Set{
		{Kind: gvk.Service, Name: "name", Namespace: "namespace"}:         sets.New(firstEntry.key),
		{Kind: gvk.DestinationRule, Name: "name", Namespace: "namespace"}: sets.New(firstEntry.key),
		{Kind: gvk.Gateway, Name: "name", Namespace: "namespace"}:         sets.New(firstEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[config.GroupVersionKind]sets.Set{
		gvk.Service:         sets.New(firstEntry.key),
		gvk.DestinationRule: sets.New(firstEntry.key),
		gvk.Gateway:         sets.New(firstEntry.key),
	})

	// add secondEntry again
	c.Add(&secondEntry, &PushRequest{Start: zeroTime.Add(time.Duration(4))}, res)

	assert.Equal(t, cache.store.Len(), 2)
	assert.Equal(t, len(cache.configIndex), 5)
	assert.Equal(t, len(cache.typesIndex), 5)
	assert.Equal(t, cache.configIndex, map[ConfigKey]sets.Set{
		{Kind: gvk.Service, Name: "name", Namespace: "namespace"}:         sets.New(firstEntry.key, secondEntry.key),
		{Kind: gvk.DestinationRule, Name: "name", Namespace: "namespace"}: sets.New(firstEntry.key),
		{Kind: gvk.Gateway, Name: "name", Namespace: "namespace"}:         sets.New(firstEntry.key),
		{Kind: gvk.EnvoyFilter, Name: "name", Namespace: "namespace"}:     sets.New(secondEntry.key),
		{Kind: gvk.WasmPlugin, Name: "name", Namespace: "namespace"}:      sets.New(secondEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[config.GroupVersionKind]sets.Set{
		gvk.Service:         sets.New(firstEntry.key, secondEntry.key),
		gvk.DestinationRule: sets.New(firstEntry.key),
		gvk.Gateway:         sets.New(firstEntry.key),
		gvk.EnvoyFilter:     sets.New(secondEntry.key),
		gvk.WasmPlugin:      sets.New(secondEntry.key),
	})

	// clear only Service dependencies, should clear both firstEntry and secondEntry references
	cache.Clear(map[ConfigKey]struct{}{{Kind: gvk.Service, Name: "name", Namespace: "namespace"}: {}})

	assert.Equal(t, len(cache.configIndex), 0)
	assert.Equal(t, len(cache.typesIndex), 0)
	assert.Equal(t, cache.store.Len(), 0)
	assert.Equal(t, cache.configIndex, map[ConfigKey]sets.Set{})
	assert.Equal(t, cache.typesIndex, map[config.GroupVersionKind]sets.Set{})
}

func TestCacheClearAll(t *testing.T) {
	test.SetIntForTest(t, &features.XDSCacheMaxSize, 10)
	zeroTime := time.Time{}
	res := &discovery.Resource{Name: "test"}
	req1 := &PushRequest{Start: zeroTime.Add(time.Duration(1))}
	req2 := &PushRequest{Start: zeroTime.Add(time.Duration(2))}
	firstEntry := entry{
		key:            "key1",
		dependentTypes: []config.GroupVersionKind{gvk.Service, gvk.DestinationRule, gvk.Gateway},
		dependentConfigs: []ConfigKey{
			{Kind: gvk.Service, Name: "name", Namespace: "namespace"},
			{Kind: gvk.DestinationRule, Name: "name", Namespace: "namespace"},
			{Kind: gvk.Gateway, Name: "name", Namespace: "namespace"},
		},
	}

	// second entry has different key and dependencies
	secondEntry := entry{
		key:            "key2",
		dependentTypes: []config.GroupVersionKind{gvk.Service, gvk.EnvoyFilter, gvk.WasmPlugin},
		dependentConfigs: []ConfigKey{
			{Kind: gvk.Service, Name: "name", Namespace: "namespace"},
			{Kind: gvk.EnvoyFilter, Name: "name", Namespace: "namespace"},
			{Kind: gvk.WasmPlugin, Name: "name", Namespace: "namespace"},
		},
	}

	c := NewXdsCache()
	cache := c.(*lruCache)

	c.Add(&firstEntry, req1, res)
	c.Add(&secondEntry, req2, res)

	// indexes populated
	assert.Equal(t, len(cache.configIndex), 5)
	assert.Equal(t, len(cache.typesIndex), 5)
	assert.Equal(t, cache.configIndex, map[ConfigKey]sets.Set{
		{Kind: gvk.Service, Name: "name", Namespace: "namespace"}:         sets.New(firstEntry.key, secondEntry.key),
		{Kind: gvk.DestinationRule, Name: "name", Namespace: "namespace"}: sets.New(firstEntry.key),
		{Kind: gvk.Gateway, Name: "name", Namespace: "namespace"}:         sets.New(firstEntry.key),
		{Kind: gvk.EnvoyFilter, Name: "name", Namespace: "namespace"}:     sets.New(secondEntry.key),
		{Kind: gvk.WasmPlugin, Name: "name", Namespace: "namespace"}:      sets.New(secondEntry.key),
	})
	assert.Equal(t, cache.typesIndex, map[config.GroupVersionKind]sets.Set{
		gvk.Service:         sets.New(firstEntry.key, secondEntry.key),
		gvk.DestinationRule: sets.New(firstEntry.key),
		gvk.Gateway:         sets.New(firstEntry.key),
		gvk.EnvoyFilter:     sets.New(secondEntry.key),
		gvk.WasmPlugin:      sets.New(secondEntry.key),
	})

	cache.ClearAll()

	// no change on empty clear
	assert.Equal(t, len(cache.configIndex), 0)
	assert.Equal(t, len(cache.typesIndex), 0)
	assert.Equal(t, cache.store.Len(), 0)
}
