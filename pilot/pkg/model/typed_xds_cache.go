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
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/go-cmp/cmp"
	"github.com/maypok86/otter"
	"github.com/puzpuzpuz/xsync/v3"
	"google.golang.org/protobuf/testing/protocmp"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/monitoring"
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
	// FlushStats clears the evicted indexes.
	FlushStats()
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
	// closes cache, frees all resources
	Close()
}

// newTypedXdsCache returns an instance of a cache.
func newTypedXdsCache[K comparable]() typedXdsCache[K] {
	cache := &lruCache[K]{
		enableAssertions: features.EnableUnsafeAssertions,
		configIndex:      xsync.NewMapOf[ConfigHash, sets.Set[K]](),
	}
	cache.store = newLru2(cache.onEvict)
	return cache
}

type lruCache[K comparable] struct {
	enableAssertions bool
	store            otter.Cache[K, cacheValue]
	// token stores the latest token of the store, used to prevent stale data overwrite.
	// It is refreshed when Clear or ClearAll are called
	token CacheToken
	// we need this mutex to protect operations which require unsafe modifications
	mu          xsync.RBMutex
	configIndex *xsync.MapOf[ConfigHash, sets.Set[K]]
}

var _ typedXdsCache[uint64] = &lruCache[uint64]{}

func newLru2[K comparable](evictCallback func(key K, value cacheValue, cause otter.DeletionCause)) otter.Cache[K, cacheValue] {
	sz := features.XDSCacheMaxSize
	if sz <= 0 {
		sz = 20000
	}
	cache, err := otter.MustBuilder[K, cacheValue](sz).
		Cost(func(key K, value cacheValue) uint32 {
			return 1
		}).
		DeletionListener(func(key K, value cacheValue, cause otter.DeletionCause) {
			evictCallback(key, value, cause)
		}).
		Build()
	if err != nil {
		panic(fmt.Errorf("invalid lru configuration: %v", err))
	}
	return cache
}

func (l *lruCache[K]) FlushStats() {
	l.recordDependentConfigSize()
}

func (l *lruCache[K]) Close() {
	l.store.Close()
}

func (l *lruCache[K]) recordDependentConfigSize() {
	if !enableStats() {
		return
	}

	// Range allows for concurrent modifications while iterating, this would cause a race reading values,
	// so we need to obtain a write lock
	l.mu.Lock()
	defer l.mu.Unlock()
	dsize := 0
	l.configIndex.Range(func(k ConfigHash, v sets.Set[K]) bool {
		dsize += len(v)
		return true
	})
	dependentConfigSize.Record(float64(dsize))
}

// This is the callback passed to LRU, it will be called whenever a key is removed.
func (l *lruCache[K]) onEvict(k K, v cacheValue, cause otter.DeletionCause) {
	if cause == otter.Size {
		xdsCacheEvictionsOnSize.Increment()
	}
	if cause == otter.Explicit {
		xdsCacheEvictionsOnClear.Increment()
	}

	rtoken := l.mu.RLock()
	defer l.mu.RUnlock(rtoken)
	l.clearConfigIndex(k, v.dependentConfigs)
}

func (l *lruCache[K]) updateConfigIndex(k K, dependentConfigs []ConfigHash) {
	for _, cfg := range dependentConfigs {
		l.configIndex.Compute(cfg, func(oldValue sets.Set[K], loaded bool) (newValue sets.Set[K], delete bool) {
			if !loaded {
				return sets.New(k), false
			}

			// we can safely modify the set in place because
			// xsync.MapOf ensures that no concurrent modifications are happening
			// over the same key at the same time
			return oldValue.Insert(k), false
		})
	}
}

func (l *lruCache[K]) clearConfigIndex(k K, dependentConfigs []ConfigHash) {
	c, exists := l.store.Get(k)

	computeFunc := func(oldValue sets.Set[K], loaded bool) (newValue sets.Set[K], delete bool) {
		// value is not present, we don't have anything to delete
		if !loaded {
			return nil, true
		}

		// we can safely modify the set in place because
		// xsync.MapOf ensures that no concurrent modifications are happening
		// over the same key at the same time
		set := oldValue.Delete(k)
		if !set.IsEmpty() {
			return set, false
		}

		// set is empty, we can delete the key
		return nil, true
	}

	if exists {
		newDependents := sets.New(c.dependentConfigs...)
		for _, cfg := range dependentConfigs {
			// we only need to clear configs {old difference new}
			if newDependents.Contains(cfg) {
				continue
			}

			l.configIndex.Compute(cfg, computeFunc)
		}
		return
	}
	for _, cfg := range dependentConfigs {
		l.configIndex.Compute(cfg, computeFunc)
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
	rtoken := l.mu.RLock()
	defer l.mu.RUnlock(rtoken)
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
	// replacing a value that already exists triggers the deletion callback for the previous value,
	// this automatically cleans the config indexes
	l.store.Set(k, toWrite)
	l.token = token
	l.updateConfigIndex(k, dependentConfigs)

	size(l.store.Size())
}

type cacheValue struct {
	value            *discovery.Resource
	token            CacheToken
	dependentConfigs []ConfigHash
}

// Get return the cached value if it exists.
func (l *lruCache[K]) Get(key K) *discovery.Resource {
	rtoken := l.mu.RLock()
	defer l.mu.RUnlock(rtoken)
	cv, ok := l.store.Get(key)
	if !ok || cv.value == nil {
		miss()
		return nil
	}

	hit()
	return cv.value
}

func (l *lruCache[K]) Clear(configs sets.Set[ConfigKey]) {
	rtoken := l.mu.RLock()
	defer l.mu.RUnlock(rtoken)
	for ckey := range configs {
		hc := ckey.HashCode()
		// we can safely use the returned value because it's no longer referenced anywhere
		// because we deleted it
		referenced, ok := l.configIndex.LoadAndDelete(hc)
		if ok {
			for key := range referenced {
				l.store.Delete(key)
			}
		}
	}
	size(l.store.Size())
}

func (l *lruCache[K]) ClearAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Purge with an evict function would turn up to be pretty slow since
	// it runs the function for every key in the store, might be better to just
	// create a new store.
	l.store.Close()
	l.store = newLru2(l.onEvict)
	l.configIndex = xsync.NewMapOf[ConfigHash, sets.Set[K]]()

	size(l.store.Size())
}

func (l *lruCache[K]) Keys() []K {
	rtoken := l.mu.RLock()
	defer l.mu.RUnlock(rtoken)
	keys := make([]K, 0, l.store.Size())
	l.store.Range(func(k K, v cacheValue) bool {
		keys = append(keys, k)
		return true
	})
	return keys
}

func (l *lruCache[K]) Snapshot() []*discovery.Resource {
	rtoken := l.mu.RLock()
	defer l.mu.RUnlock(rtoken)

	res := make([]*discovery.Resource, 0, l.store.Size())
	l.store.Range(func(k K, v cacheValue) bool {
		res = append(res, v.value)
		return true
	})

	return res
}

func (l *lruCache[K]) indexLength() int {
	rtoken := l.mu.RLock()
	defer l.mu.RUnlock(rtoken)
	return l.configIndex.Size()
}

func (l *lruCache[K]) configIndexSnapshot() map[ConfigHash]sets.Set[K] {
	// Range allows for concurrent modifications while iterating, this would cause a race reading values,
	// so we need to obtain a write lock
	l.mu.Lock()
	defer l.mu.Unlock()
	res := make(map[ConfigHash]sets.Set[K], l.configIndex.Size())
	l.configIndex.Range(func(k ConfigHash, v sets.Set[K]) bool {
		res[k] = v
		return true
	})
	return res
}

// disabledCache is a cache that is always empty
type disabledCache[K comparable] struct{}

var _ typedXdsCache[uint64] = &disabledCache[uint64]{}

func (d disabledCache[K]) FlushStats() {
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

func (d disabledCache[K]) Close() {
}
