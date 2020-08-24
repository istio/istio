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
	"sync"

	"github.com/golang/protobuf/ptypes/any"

	"istio.io/istio/pilot/pkg/util/sets"
)

// XdsCacheEntry interface defines functions that should be implemented by
// resources that can be cached.
type XdsCacheEntry interface {
	// Key is the key to be used in cache.
	Key() string
	// DependentConfigs is config items that this cache key is dependent on.
	// Whenever these configs change, we should invalidate this cache entry.
	DependentConfigs() []ConfigKey
	// Cacheable indicates whether this entry is valid for cache. For example
	// for EDS to be cacheable, the Endpoint should have corresponding service.
	Cacheable() bool
}

// XdsCache interface defines a store for caching XDS responses.
// All operations are thread safe.
type XdsCache interface {
	// Add adds the given XdsCacheEntry with the value to the cache.
	Add(entry XdsCacheEntry, value *any.Any)
	// Get retrieves the cached value if it exists. The boolean indicates
	// whether the entry exists in the cache.
	Get(entry XdsCacheEntry) (*any.Any, bool)
	// Clear removes the cache entries that are dependent on the configs passed.
	Clear(map[ConfigKey]struct{})
	// ClearAll clears the entire cache.
	ClearAll()
	// Keys returns all currently configured keys. This is for testing/debug only
	Keys() []string
}

// inMemoryCache is a simple implementation of Cache that uses in memory map.
type inMemoryCache struct {
	store       map[string]*any.Any
	configIndex map[ConfigKey]sets.Set
	mu          sync.RWMutex
}

// New returns an instance of a cache.
func NewXdsCache() XdsCache {
	return &inMemoryCache{
		store:       map[string]*any.Any{},
		configIndex: map[ConfigKey]sets.Set{},
	}
}

func (c *inMemoryCache) Add(entry XdsCacheEntry, value *any.Any) {
	if !entry.Cacheable() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	k := entry.Key()
	c.store[k] = value
	for _, config := range entry.DependentConfigs() {
		if c.configIndex[config] == nil {
			c.configIndex[config] = sets.NewSet()
		}
		c.configIndex[config].Insert(k)
	}
}

func (c *inMemoryCache) Get(entry XdsCacheEntry) (*any.Any, bool) {
	if !entry.Cacheable() {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	k, f := c.store[entry.Key()]
	return k, f
}

func (c *inMemoryCache) Clear(configs map[ConfigKey]struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for ckey := range configs {
		referenced := c.configIndex[ckey]
		delete(c.configIndex, ckey)
		for keys := range referenced {
			delete(c.store, keys)
		}
	}
}

func (c *inMemoryCache) ClearAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = map[string]*any.Any{}
	c.configIndex = map[ConfigKey]sets.Set{}
}

func (c *inMemoryCache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	keys := []string{}
	for k := range c.store {
		keys = append(keys, k)
	}
	return keys
}

// DisabledCache is a cache that is always empty
type DisabledCache struct{}

var _ XdsCache = &DisabledCache{}

func (d DisabledCache) Add(key XdsCacheEntry, value *any.Any) {}

func (d DisabledCache) Get(XdsCacheEntry) (*any.Any, bool) {
	return nil, false
}

func (d DisabledCache) Clear(configsUpdated map[ConfigKey]struct{}) {}

func (d DisabledCache) ClearAll() {}

func (d DisabledCache) Keys() []string { return nil }
