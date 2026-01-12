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
	"github.com/hashicorp/golang-lru/v2/simplelru"
	"google.golang.org/protobuf/testing/protocmp"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

var enableStats = func() bool {
	return features.EnableXDSCacheMetrics
}

var (
	xdsCacheReads = monitoring.NewSum(
		"xds_cache_reads",
		"Total number of xds cache xdsCacheReads.",
		monitoring.WithEnabled(enableStats),
	)

	xdsCacheEvictions = monitoring.NewSum(
		"xds_cache_evictions",
		"Total number of xds cache evictions.",
		monitoring.WithEnabled(enableStats),
	)

	xdsCacheSize = monitoring.NewGauge(
		"xds_cache_size",
		"Current size of xds cache",
		monitoring.WithEnabled(enableStats),
	)

	dependentConfigSize = monitoring.NewGauge(
		"xds_cache_dependent_config_size",
		"Current size of dependent configs",
		monitoring.WithEnabled(enableStats),
	)

	xdsCacheHits             = xdsCacheReads.With(typeTag.Value("hit"))
	xdsCacheMisses           = xdsCacheReads.With(typeTag.Value("miss"))
	xdsCacheEvictionsOnClear = xdsCacheEvictions.With(typeTag.Value("clear"))
	xdsCacheEvictionsOnSize  = xdsCacheEvictions.With(typeTag.Value("size"))
)

func hit() {
	xdsCacheHits.Increment()
}

func miss() {
	xdsCacheMisses.Increment()
}

func size(cs int) {
	xdsCacheSize.Record(float64(cs))
}

type CacheToken uint64

type dependents interface {
	DependentConfigs() []ConfigHash
}

// typedXdsCache interface defines a store for caching XDS responses.
// All operations are thread safe.
type typedXdsCache[K comparable] interface {
	// Flush clears the evicted indexes.
	Flush()
	// Add adds the given key with the value and its dependents for the given pushContext to the cache.
	// If the cache has been updated to a newer push context, the write will be dropped silently.
	// This ensures stale data does not overwrite fresh data when dealing with concurrent
	// writers.
	Add(key K, entry dependents, pushRequest *PushRequest, value *discovery.Resource)
	// Get retrieves the cached value if it exists.
	Get(key K) *discovery.Resource
	// Clear removes the cache entries that are dependent on the configs passed.
	Clear(sets.Set[ConfigKey])
	// ClearAll clears the entire cache.
	ClearAll()
	// Keys returns all currently configured keys. This is for testing/debug only
	Keys() []K
	// Snapshot returns a snapshot of all keys and values. This is for testing/debug only
	Snapshot() []*discovery.Resource
}

// newTypedXdsCache returns an instance of a cache.
func newTypedXdsCache[K comparable]() typedXdsCache[K] {
	cache := &lruCache[K]{
		enableAssertions: features.EnableUnsafeAssertions,
		configIndex:      map[ConfigHash]sets.Set[K]{},
		evictQueue:       make([]evictKeyConfigs[K], 0, 1000),
	}
	cache.store = newLru(cache.onEvict)
	return cache
}

type evictKeyConfigs[K comparable] struct {
	key              K
	dependentConfigs []ConfigHash
}

type lruCache[K comparable] struct {
	enableAssertions bool
	store            simplelru.LRUCache[K, cacheValue]
	// token stores the latest token of the store, used to prevent stale data overwrite.
	// It is refreshed when Clear or ClearAll are called
	token       CacheToken
	mu          sync.RWMutex
	configIndex map[ConfigHash]sets.Set[K]

	evictQueue []evictKeyConfigs[K]

	// mark whether a key is evicted on Clear call, passively.
	evictedOnClear bool
}

var _ typedXdsCache[uint64] = &lruCache[uint64]{}

func newLru[K comparable](evictCallback simplelru.EvictCallback[K, cacheValue]) simplelru.LRUCache[K, cacheValue] {
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

func (l *lruCache[K]) Flush() {
	l.mu.Lock()
	for _, keyConfigs := range l.evictQueue {
		l.clearConfigIndex(keyConfigs.key, keyConfigs.dependentConfigs)
	}
	// The underlying array releases references to elements so that they can be garbage collected.
	clear(l.evictQueue)
	l.evictQueue = l.evictQueue[:0:1000]

	l.recordDependentConfigSize()
	l.mu.Unlock()
}

func (l *lruCache[K]) recordDependentConfigSize() {
	if !enableStats() {
		return
	}
	dsize := 0
	for _, dependents := range l.configIndex {
		dsize += len(dependents)
	}
	dependentConfigSize.Record(float64(dsize))
}

// This is the callback passed to LRU, it will be called whenever a key is removed.
func (l *lruCache[K]) onEvict(k K, v cacheValue) {
	if l.evictedOnClear {
		xdsCacheEvictionsOnClear.Increment()
	} else {
		xdsCacheEvictionsOnSize.Increment()
	}

	// async clearing indexes
	l.evictQueue = append(l.evictQueue, evictKeyConfigs[K]{k, v.dependentConfigs})
}

func (l *lruCache[K]) updateConfigIndex(k K, dependentConfigs []ConfigHash) {
	for _, cfg := range dependentConfigs {
		sets.InsertOrNew(l.configIndex, cfg, k)
	}
}

func (l *lruCache[K]) clearConfigIndex(k K, dependentConfigs []ConfigHash) {
	c, exists := l.store.Get(k)
	if exists {
		newDependents := c.dependentConfigs
		// we only need to clear configs {old difference new}
		dependents := sets.New(dependentConfigs...).DifferenceInPlace(sets.New(newDependents...))
		for cfg := range dependents {
			sets.DeleteCleanupLast(l.configIndex, cfg, k)
		}
		return
	}
	for _, cfg := range dependentConfigs {
		sets.DeleteCleanupLast(l.configIndex, cfg, k)
	}
}

// assertUnchanged checks that a cache entry is not changed. This helps catch bad cache invalidation
// We should never have a case where we overwrite an existing item with a new change. Instead, when
// config sources change, Clear/ClearAll should be called. At this point, we may get multiple writes
// because multiple writers may get cache misses concurrently, but they ought to generate identical
// configuration. This also checks that our XDS config generation is deterministic, which is a very
// important property.
func (l *lruCache[K]) assertUnchanged(key K, existing *discovery.Resource, replacement *discovery.Resource) {
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

func (l *lruCache[K]) Add(k K, entry dependents, pushReq *PushRequest, value *discovery.Resource) {
	if pushReq == nil || pushReq.Start.Equal(time.Time{}) {
		return
	}
	// It will not overflow until year 2262
	token := CacheToken(pushReq.Start.UnixNano())
	l.mu.Lock()
	defer l.mu.Unlock()
	if token < l.token {
		// entry may be stale, we need to drop it. This can happen when the cache is invalidated
		// after we call Clear or ClearAll.
		return
	}
	cur, f := l.store.Get(k)
	if f {
		// This is the stale or same resource
		if token <= cur.token {
			return
		}
		if l.enableAssertions {
			l.assertUnchanged(k, cur.value, value)
		}
	}

	dependentConfigs := entry.DependentConfigs()
	toWrite := cacheValue{value: value, token: token, dependentConfigs: dependentConfigs}
	l.store.Add(k, toWrite)
	l.token = token
	l.updateConfigIndex(k, dependentConfigs)

	// we have to make sure we evict old entries with the same key
	// to prevent leaking in the index maps
	if f {
		l.evictQueue = append(l.evictQueue, evictKeyConfigs[K]{k, cur.dependentConfigs})
	}
	size(l.store.Len())
}

type cacheValue struct {
	value            *discovery.Resource
	token            CacheToken
	dependentConfigs []ConfigHash
}

func (l *lruCache[K]) Get(key K) *discovery.Resource {
	return l.get(key, 0)
}

// get return the cached value if it exists.
func (l *lruCache[K]) get(key K, token CacheToken) *discovery.Resource {
	// DON'T try to refactor to use RLock here.
	// RLock will cause panic because hashicorp LRU cache does not guarantee concurrent safe.
	l.mu.Lock()
	defer l.mu.Unlock()
	cv, ok := l.store.Get(key)
	if !ok || cv.value == nil {
		miss()
		return nil
	}
	if cv.token >= token {
		hit()
		return cv.value
	}
	miss()
	return nil
}

func (l *lruCache[K]) Clear(configs sets.Set[ConfigKey]) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.token = CacheToken(time.Now().UnixNano())
	l.evictedOnClear = true
	defer func() {
		l.evictedOnClear = false
	}()
	for ckey := range configs {
		hc := ckey.HashCode()
		referenced := l.configIndex[hc]
		delete(l.configIndex, hc)
		for key := range referenced {
			l.store.Remove(key)
		}
	}
	size(l.store.Len())
}

func (l *lruCache[K]) ClearAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.token = CacheToken(time.Now().UnixNano())
	// Purge with an evict function would turn up to be pretty slow since
	// it runs the function for every key in the store, might be better to just
	// create a new store.
	l.store = newLru(l.onEvict)
	l.configIndex = map[ConfigHash]sets.Set[K]{}

	// The underlying array releases references to elements so that they can be garbage collected.
	clear(l.evictQueue)
	l.evictQueue = l.evictQueue[:0:1000]

	size(l.store.Len())
}

func (l *lruCache[K]) Keys() []K {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return slices.Clone(l.store.Keys())
}

func (l *lruCache[K]) Snapshot() []*discovery.Resource {
	l.mu.RLock()
	defer l.mu.RUnlock()
	iKeys := l.store.Keys()
	res := make([]*discovery.Resource, len(iKeys))
	for i, ik := range iKeys {
		v, ok := l.store.Get(ik)
		if !ok {
			continue
		}

		res[i] = v.value
	}
	return res
}

func (l *lruCache[K]) indexLength() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.configIndex)
}

func (l *lruCache[K]) configIndexSnapshot() map[ConfigHash]sets.Set[K] {
	l.mu.RLock()
	defer l.mu.RUnlock()
	res := make(map[ConfigHash]sets.Set[K], len(l.configIndex))
	for k, v := range l.configIndex {
		res[k] = v
	}
	return res
}

// disabledCache is a cache that is always empty
type disabledCache[K comparable] struct{}

var _ typedXdsCache[uint64] = &disabledCache[uint64]{}

func (d disabledCache[K]) Flush() {
}

func (d disabledCache[K]) Add(k K, entry dependents, pushReq *PushRequest, value *discovery.Resource) {
}

func (d disabledCache[K]) Get(k K) *discovery.Resource {
	return nil
}

func (d disabledCache[K]) Clear(configsUpdated sets.Set[ConfigKey]) {}

func (d disabledCache[K]) ClearAll() {}

func (d disabledCache[K]) Keys() []K { return nil }

func (d disabledCache[K]) Snapshot() []*discovery.Resource { return nil }
