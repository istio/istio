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
	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/golang-lru/simplelru"
	"go.uber.org/atomic"
	"google.golang.org/protobuf/testing/protocmp"

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

type CacheToken uint64

// XdsCache interface defines a store for caching XDS responses.
// All operations are thread safe.
type XdsCache interface {
	// Add adds the given XdsCacheEntry with the value to the cache. A token, returned from Get, must
	// be included or writes will be (silently) dropped. Additionally, if the cache has been
	// invalided between when a token is fetched from Get and when Add is called, the write will be
	// dropped. This ensures stale data does not overwrite fresh data when dealing with concurrent
	// writers.
	Add(entry XdsCacheEntry, token CacheToken, value *any.Any)
	// Get retrieves the cached value if it exists. The boolean indicates
	// whether the entry exists in the cache.
	//
	// A CacheToken is additionally included in the response. This must be used for subsequent writes
	// to this key. This ensures that if the cache is invalidated between our read and write, we do
	// not persist stale data.
	//
	// Standard usage:
	// if obj, token, f := cache.Get(key); f { ...do something... }
	// else { computed := expensive(); cache.Add(key, token, computed); }
	Get(entry XdsCacheEntry) (*any.Any, CacheToken, bool)
	// Clear removes the cache entries that are dependent on the configs passed.
	Clear(map[ConfigKey]struct{})
	// ClearAll clears the entire cache.
	ClearAll()
	// Keys returns all currently configured keys. This is for testing/debug only
	Keys() []string
}

// NewXdsCache returns an instance of a cache.
func NewXdsCache() XdsCache {
	return &lruCache{
		enableAssertions: features.EnableUnsafeAssertions,
		store:            newLru(),
		configIndex:      map[ConfigKey]sets.Set{},
		nextToken:        atomic.NewUint64(0),
	}
}

// NewLenientXdsCache returns an instance of a cache that does not validate token based get/set and enable assertions.
func NewLenientXdsCache() XdsCache {
	return &lruCache{
		enableAssertions: false,
		store:            newLru(),
		configIndex:      map[ConfigKey]sets.Set{},
		nextToken:        atomic.NewUint64(0),
	}
}

type lruCache struct {
	enableAssertions bool
	store            simplelru.LRUCache
	// nextToken stores the next token to use. The content here doesn't matter, we just need a cheap
	// unique identifier.
	nextToken   *atomic.Uint64
	mu          sync.RWMutex
	configIndex map[ConfigKey]sets.Set
}

var _ XdsCache = &lruCache{}

func newLru() simplelru.LRUCache {
	sz := features.XDSCacheMaxSize
	if sz <= 0 {
		sz = 20000
	}
	l, err := simplelru.NewLRU(sz, evict)
	if err != nil {
		panic(fmt.Errorf("invalid lru configuration: %v", err))
	}
	return l
}

// assertUnchanged checks that a cache entry is not changed. This helps catch bad cache invalidation
// We should never have a case where we overwrite an existing item with a new change. Instead, when
// config sources change, Clear/ClearAll should be called. At this point, we may get multiple writes
// because multiple writers may get cache misses concurrently, but they ought to generate identical
// configuration. This also checks that our XDS config generation is deterministic, which is a very
// important property.
func (l *lruCache) assertUnchanged(existing *any.Any, replacement *any.Any) {
	if l.enableAssertions {
		if existing == nil {
			// This is a new addition, not an update
			return
		}
		if !cmp.Equal(existing, replacement, protocmp.Transform()) {
			warning := fmt.Errorf("assertion failed, cache entry changed but not cleared: %v\n%v\n%v",
				cmp.Diff(existing, replacement, protocmp.Transform()), existing, replacement)
			panic(warning)
		}
	}
}

func (l *lruCache) Add(entry XdsCacheEntry, token CacheToken, value *any.Any) {
	if !entry.Cacheable() {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	k := entry.Key()
	cur, f := l.store.Get(k)
	toWrite := cacheValue{value: value}
	if f {
		if token != cur.(cacheValue).token {
			// entry may be stale, we need to drop it. This can happen when the cache is invalidated
			// after we call Get.
			return
		}
		// Otherwise, make sure we write the current token again. We don't change the key on writes; the
		// same token will be used for a value until its invalidated
		toWrite.token = cur.(cacheValue).token
	} else {
		// This is our first time seeing this; this means it was invalidated recently and this is our
		// first write, or we forgot to call Get before.
		return
	}
	if l.enableAssertions {
		if toWrite.token == 0 {
			panic("token cannot be empty. was Get() called before Add()?")
		}
		l.assertUnchanged(cur.(cacheValue).value, value)
	}
	l.store.Add(k, toWrite)
	indexConfig(l.configIndex, entry.Key(), entry)
	size(l.store.Len())
}

type cacheValue struct {
	value *any.Any
	token CacheToken
}

func (l *lruCache) Get(entry XdsCacheEntry) (*any.Any, CacheToken, bool) {
	if !entry.Cacheable() {
		return nil, 0, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	k := entry.Key()
	val, ok := l.store.Get(k)
	if !ok {
		miss()
		// If the entry is not found at all, this is our first read of it. We will generate and store
		// a new token. Subsequent writes must include it.
		tok := CacheToken(l.nextToken.Inc())
		l.store.Add(k, cacheValue{token: tok})
		return nil, tok, false
	}
	cv := val.(cacheValue)
	if cv.value == nil {
		miss()
		// We have generated a token previously, so return that, but this is still a cache miss as
		// no value is stored.
		return nil, cv.token, false
	}
	hit()
	return cv.value, cv.token, true
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

func (d DisabledCache) Add(key XdsCacheEntry, token CacheToken, value *any.Any) {}

func (d DisabledCache) Get(XdsCacheEntry) (*any.Any, CacheToken, bool) {
	return nil, 0, false
}

func (d DisabledCache) Clear(configsUpdated map[ConfigKey]struct{}) {}

func (d DisabledCache) ClearAll() {}

func (d DisabledCache) Keys() []string { return nil }
