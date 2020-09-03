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
	"fmt"
	"sync"

	"github.com/golang/protobuf/ptypes/any"
	"github.com/hashicorp/golang-lru/simplelru"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/pkg/monitoring"
)

func init() {
	monitoring.MustRegister(xdsCacheReads)
	monitoring.MustRegister(xdsCacheEvictions)
	monitoring.MustRegister(xdsCacheSize)
}

var (
	xdsCacheReads = monitoring.NewSum(
		"xds_cache_reads",
		"Total number of xds cache xdsCacheReads.",
		monitoring.WithLabels(typeTag),
	)

	xdsCacheEvictions = monitoring.NewSum(
		"xds_cache_evictions",
		"Total number of xds cache evictions.",
	)

	xdsCacheSize = monitoring.NewGauge(
		"xds_cache_size",
		"Current size of xds cache",
	)

	xdsCacheHits   = xdsCacheReads.With(typeTag.Value("hit"))
	xdsCacheMisses = xdsCacheReads.With(typeTag.Value("miss"))
)

func hit() {
	if features.EnableXDSCacheMetrics {
		xdsCacheHits.Increment()
	}
}

func miss() {
	if features.EnableXDSCacheMetrics {
		xdsCacheMisses.Increment()
	}
}

func evict(k interface{}, v interface{}) {
	if features.EnableXDSCacheMetrics {
		xdsCacheEvictions.Increment()
	}
}

func size(cs int) {
	if features.EnableXDSCacheMetrics {
		xdsCacheSize.Record(float64(cs))
	}
}

func indexConfig(configIndex map[ConfigKey]sets.Set, k string, entry XdsCacheEntry) {
	for _, config := range entry.DependentConfigs() {
		if configIndex[config] == nil {
			configIndex[config] = sets.NewSet()
		}
		configIndex[config].Insert(k)
	}
}

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

// NewXdsCache returns an instance of a cache.
func NewXdsCache() XdsCache {
	if features.XDSCacheMaxSize <= 0 {
		return &inMemoryCache{
			store:       map[string]*any.Any{},
			configIndex: map[ConfigKey]sets.Set{},
		}
	}
	return &lruCache{
		store:       newLru(),
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
	indexConfig(c.configIndex, k, entry)
	size(len(c.store))
}

func (c *inMemoryCache) Get(entry XdsCacheEntry) (*any.Any, bool) {
	if !entry.Cacheable() {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	k, f := c.store[entry.Key()]
	if f {
		hit()
	} else {
		miss()
	}
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
	size(len(c.store))
}

func (c *inMemoryCache) ClearAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = map[string]*any.Any{}
	c.configIndex = map[ConfigKey]sets.Set{}
	size(len(c.store))
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

type lruCache struct {
	store simplelru.LRUCache

	mu          sync.RWMutex
	configIndex map[ConfigKey]sets.Set
}

var _ XdsCache = &lruCache{}

func newLru() simplelru.LRUCache {
	l, err := simplelru.NewLRU(features.XDSCacheMaxSize, evict)
	if err != nil {
		panic(fmt.Errorf("invalid lru configuration: %v", err))
	}
	return l
}

func (l *lruCache) Add(entry XdsCacheEntry, value *any.Any) {
	if !entry.Cacheable() {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	k := entry.Key()
	l.store.Add(k, value)
	indexConfig(l.configIndex, entry.Key(), entry)
	size(l.store.Len())
}

func (l *lruCache) Get(entry XdsCacheEntry) (*any.Any, bool) {
	if !entry.Cacheable() {
		return nil, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	val, ok := l.store.Get(entry.Key())
	if !ok {
		miss()
		return nil, false
	}
	hit()
	return val.(*any.Any), true
}

func (l *lruCache) Clear(configs map[ConfigKey]struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for ckey := range configs {
		referenced := l.configIndex[ckey]
		delete(l.configIndex, ckey)
		for key := range referenced {
			l.store.Remove(key)
		}
	}
	size(l.store.Len())
}

func (l *lruCache) ClearAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.store.Purge()
	l.configIndex = map[ConfigKey]sets.Set{}
	size(l.store.Len())
}

func (l *lruCache) Keys() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	iKeys := l.store.Keys()
	keys := make([]string, 0, len(iKeys))
	for _, ik := range iKeys {
		keys = append(keys, ik.(string))
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
