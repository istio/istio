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
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/golang-lru/simplelru"
	"google.golang.org/protobuf/testing/protocmp"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/sets"
	"istio.io/pkg/monitoring"
)

func init() {
	monitoring.MustRegister(xdsCacheReads)
	monitoring.MustRegister(xdsCacheEvictions)
	monitoring.MustRegister(xdsCacheSize)
	monitoring.MustRegister(dependentConfigSize)
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

	dependentConfigSize = monitoring.NewGauge(
		"xds_cache_dependent_config_size",
		"Current size of dependent configs",
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

func size(cs int) {
	if features.EnableXDSCacheMetrics {
		xdsCacheSize.Record(float64(cs))
	}
}

// XdsCacheEntry interface defines functions that should be implemented by
// resources that can be cached.
type XdsCacheEntry interface {
	// Key is the key to be used in cache.
	Key() string
	// DependentTypes are config types that this cache key is dependent on.
	// Whenever any configs of this type changes, we should invalidate this cache entry.
	// Note: DependentConfigs should be preferred wherever possible.
	DependentTypes() []kind.Kind
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
	// Add adds the given XdsCacheEntry with the value for the given pushContext to the cache.
	// If the cache has been updated to a newer push context, the write will be dropped silently.
	// This ensures stale data does not overwrite fresh data when dealing with concurrent
	// writers.
	Add(entry XdsCacheEntry, pushRequest *PushRequest, value *discovery.Resource)
	// Get retrieves the cached value if it exists. The boolean indicates
	// whether the entry exists in the cache.
	Get(entry XdsCacheEntry) (*discovery.Resource, bool)
	// Clear removes the cache entries that are dependent on the configs passed.
	Clear(map[ConfigKey]struct{})
	// ClearAll clears the entire cache.
	ClearAll()
	// Keys returns all currently configured keys. This is for testing/debug only
	Keys() []string
	// Snapshot returns a snapshot of all keys and values. This is for testing/debug only
	Snapshot() map[string]*discovery.Resource
	// Run runs the cache, mainly for cleanup and is triggered asynchronously by key eviction.
	Run(stop <-chan struct{})
}

// NewXdsCache returns an instance of a cache.
func NewXdsCache() XdsCache {
	cache := &LruCache{
		enableAssertions: features.EnableUnsafeAssertions,
		configIndex:      map[ConfigKey]sets.Set{},
		typesIndex:       map[kind.Kind]sets.Set{},
		trackEvicted:     true,
		evictedKeys:      sets.New(),
		evictCh:          make(chan struct{}, 1),
	}
	cache.store = newLru(cache.onEvict)
	return cache
}

// NewLenientXdsCache returns an instance of a cache that does not validate token based get/set and enable assertions.
func NewLenientXdsCache() XdsCache {
	cache := &LruCache{
		enableAssertions: false,
		configIndex:      map[ConfigKey]sets.Set{},
		typesIndex:       map[kind.Kind]sets.Set{},
		trackEvicted:     true,
		evictedKeys:      sets.New(),
		evictCh:          make(chan struct{}, 1),
	}
	cache.store = newLru(cache.onEvict)
	return cache
}

// export LruCache for test
type LruCache struct {
	enableAssertions bool
	store            simplelru.LRUCache
	// token stores the latest token of the store, used to prevent stale data overwrite.
	// It is refreshed when Clear or ClearAll are called
	token        CacheToken
	mu           sync.RWMutex
	configIndex  map[ConfigKey]sets.Set
	typesIndex   map[kind.Kind]sets.Set
	trackEvicted bool
	evictedKeys  sets.Set
	// used to notify a key is evicted, calling Remove actively does not trigger it.
	evictCh chan struct{}
}

var _ XdsCache = &LruCache{}

func newLru(evictCallback simplelru.EvictCallback) simplelru.LRUCache {
	sz := features.XDSCacheMaxSize
	if sz <= 0 {
		sz = 20000
	}
	l, err := simplelru.NewLRU(sz, evictCallback)
	if err != nil {
		panic(fmt.Errorf("invalid lru configuration: %v", err))
	}
	return l
}

func (l *LruCache) recordDependentConfigSize() {
	if !features.EnableXDSCacheMetrics {
		return
	}
	dsize := 0
	for _, dependents := range l.configIndex {
		dsize += len(dependents)
	}
	dependentConfigSize.Record(float64(dsize))
}

// This is the callback passed to LRU, it will be called whenever a key is removed.
func (l *LruCache) onEvict(k interface{}, _ interface{}) {
	if features.EnableXDSCacheMetrics {
		xdsCacheEvictions.Increment()
	}

	if l.trackEvicted {
		l.evictedKeys.Insert(k.(string))
		select {
		case l.evictCh <- struct{}{}:
		default: // the signal has not been consumed
		}
	}
}

func (l *LruCache) handleEvicted(stopCh <-chan struct{}) {
	for {
		select {
		case <-l.evictCh:
			l.clearEvicted()
		case <-stopCh:
			log.Infof("LruCache has been stopped")
			return
		}
	}
}

// clearEvicted is to clear configIndex and typesIndex based on evictedKeys.
func (l *LruCache) clearEvicted() {
	l.mu.Lock()
	defer l.mu.Unlock()

	// In order to save cpu, if the evicted keys is very small, we can just ignore
	// TODO: make it configurable
	if len(l.evictedKeys) < 100 {
		return
	}

	for configKey, index := range l.configIndex {
		evicted := index.Intersection(l.evictedKeys)
		for k := range evicted {
			index.Delete(k)
			if index.IsEmpty() {
				delete(l.configIndex, configKey)
			}
		}
	}

	for gvk, index := range l.typesIndex {
		evicted := index.Intersection(l.evictedKeys)
		for k := range evicted {
			index.Delete(k)
			if index.IsEmpty() {
				delete(l.typesIndex, gvk)
			}
		}
	}
	l.evictedKeys = sets.New()
	l.recordDependentConfigSize()
}

func (l *LruCache) updateConfigIndex(k string, dependentConfigs []ConfigKey) {
	for _, cfg := range dependentConfigs {
		if l.configIndex[cfg] == nil {
			l.configIndex[cfg] = sets.New()
		}
		l.configIndex[cfg].Insert(k)
	}
	l.recordDependentConfigSize()
}

func (l *LruCache) clearConfigIndex(k string, dependentConfigs []ConfigKey) {
	for _, cfg := range dependentConfigs {
		index := l.configIndex[cfg]
		if index != nil {
			index.Delete(k)
			if index.IsEmpty() {
				delete(l.configIndex, cfg)
			}
		}
	}
	l.recordDependentConfigSize()
}

func (l *LruCache) updateTypesIndex(k string, dependentTypes []kind.Kind) {
	for _, t := range dependentTypes {
		if l.typesIndex[t] == nil {
			l.typesIndex[t] = sets.New()
		}
		l.typesIndex[t].Insert(k)
	}
}

func (l *LruCache) clearTypesIndex(k string, dependentTypes []kind.Kind) {
	for _, t := range dependentTypes {
		index := l.typesIndex[t]
		if index != nil {
			index.Delete(k)
			if index.IsEmpty() {
				delete(l.typesIndex, t)
			}
		}
	}
}

// Note: only used for test
func (l *LruCache) GetKeysByConfigKey(ck ConfigKey) sets.Set {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return l.configIndex[ck]
}

// assertUnchanged checks that a cache entry is not changed. This helps catch bad cache invalidation
// We should never have a case where we overwrite an existing item with a new change. Instead, when
// config sources change, Clear/ClearAll should be called. At this point, we may get multiple writes
// because multiple writers may get cache misses concurrently, but they ought to generate identical
// configuration. This also checks that our XDS config generation is deterministic, which is a very
// important property.
func (l *LruCache) assertUnchanged(key string, existing *discovery.Resource, replacement *discovery.Resource) {
	if l.enableAssertions {
		if existing == nil {
			// This is a new addition, not an update
			return
		}
		// Record time so that we can correlate when the error actually happened, since the async reporting
		// may be delayed
		t0 := time.Now()
		// This operation is really slow, which makes tests fail for unrelated reasons, so we process it async.
		go func() {
			if !cmp.Equal(existing, replacement, protocmp.Transform()) {
				warning := fmt.Errorf("assertion failed at %v, cache entry changed but not cleared for key %v: %v\n%v\n%v",
					t0, key, cmp.Diff(existing, replacement, protocmp.Transform()), existing, replacement)
				panic(warning)
			}
		}()
	}
}

func (l *LruCache) Add(entry XdsCacheEntry, pushReq *PushRequest, value *discovery.Resource) {
	if !entry.Cacheable() || pushReq == nil || pushReq.Start.Equal(time.Time{}) {
		return
	}
	// It will not overflow until year 2262
	token := CacheToken(pushReq.Start.UnixNano())
	k := entry.Key()
	l.mu.Lock()
	defer l.mu.Unlock()
	cur, f := l.store.Get(k)
	if f {
		// This is the stale resource
		if token < cur.(cacheValue).token || token < l.token {
			// entry may be stale, we need to drop it. This can happen when the cache is invalidated
			// after we call Get.
			return
		}
		if l.enableAssertions {
			l.assertUnchanged(k, cur.(cacheValue).value, value)
		}
	}

	if token < l.token {
		return
	}

	toWrite := cacheValue{value: value, token: token}
	l.store.Add(k, toWrite)
	l.token = token
	dependentConfigs := entry.DependentConfigs()
	dependentTypes := entry.DependentTypes()
	l.updateConfigIndex(k, dependentConfigs)
	l.updateTypesIndex(k, dependentTypes)
	size(l.store.Len())
}

type cacheValue struct {
	value *discovery.Resource
	token CacheToken
}

func (l *LruCache) Get(entry XdsCacheEntry) (*discovery.Resource, bool) {
	if !entry.Cacheable() {
		return nil, false
	}
	k := entry.Key()
	l.mu.Lock()
	defer l.mu.Unlock()
	val, ok := l.store.Get(k)
	if !ok {
		miss()
		return nil, false
	}
	cv := val.(cacheValue)
	if cv.value == nil {
		miss()
		return nil, false
	}
	hit()
	return cv.value, true
}

func (l *LruCache) Clear(configs map[ConfigKey]struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.token = CacheToken(time.Now().UnixNano())
	// not to record keys that are actively removed
	l.trackEvicted = false
	defer func() {
		l.trackEvicted = true
	}()
	for ckey := range configs {
		referenced := l.configIndex[ckey]
		delete(l.configIndex, ckey)
		for key := range referenced {
			l.store.Remove(key)
		}
		tReferenced := l.typesIndex[ckey.Kind]
		delete(l.typesIndex, ckey.Kind)
		for key := range tReferenced {
			l.store.Remove(key)
		}
	}
	size(l.store.Len())
}

func (l *LruCache) ClearAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.token = CacheToken(time.Now().UnixNano())
	// Purge with an onEvict function would turn up to be pretty slow since
	// it runs the function for every key in the store, might be better to just
	// create a new store.
	l.store = newLru(l.onEvict)
	l.configIndex = map[ConfigKey]sets.Set{}
	l.typesIndex = map[kind.Kind]sets.Set{}
	l.evictedKeys = sets.New()
	size(l.store.Len())
}

func (l *LruCache) Keys() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	iKeys := l.store.Keys()
	keys := make([]string, 0, len(iKeys))
	for _, ik := range iKeys {
		keys = append(keys, ik.(string))
	}
	return keys
}

func (l *LruCache) Snapshot() map[string]*discovery.Resource {
	l.mu.RLock()
	defer l.mu.RUnlock()
	iKeys := l.store.Keys()
	res := make(map[string]*discovery.Resource, len(iKeys))
	for _, ik := range iKeys {
		v, ok := l.store.Get(ik)
		if !ok {
			continue
		}

		res[ik.(string)] = v.(cacheValue).value
	}
	return res
}

func (l *LruCache) Run(stop <-chan struct{}) {
	l.handleEvicted(stop)
}

// DisabledCache is a cache that is always empty
type DisabledCache struct{}

var _ XdsCache = &DisabledCache{}

func (d DisabledCache) Add(key XdsCacheEntry, pushReq *PushRequest, value *discovery.Resource) {}

func (d DisabledCache) Get(XdsCacheEntry) (*discovery.Resource, bool) {
	return nil, false
}

func (d DisabledCache) Clear(configsUpdated map[ConfigKey]struct{}) {}

func (d DisabledCache) ClearAll() {}

func (d DisabledCache) Keys() []string { return nil }

func (d DisabledCache) Snapshot() map[string]*discovery.Resource { return nil }

func (d DisabledCache) Run(<-chan struct{}) {}
