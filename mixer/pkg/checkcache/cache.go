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

// Package checkcache provides a scalable cache to hold results of Mixer.Check operations.
//
// Entries are added into the cache by supplying an attribute bag along with a ReferencedAttributes struct
// which determines the set of attributes in the bag should be used as a cache lookup key. Entries are looked up
// from the cache using an attribute bag.
package checkcache

// TODO: This code should optimize the storage of Value. It's likely that a great many entries in the cache will
// have identical StatusMessage and ReferencedAttributes values and sharing these immutable objects across
// entries would save a substantial amount of storage relative to the whole cache size.
//
// TODO: should there be a ReferencedAttribute struct associated with each KeyShape so that it doesn't need to be stored
// with each corresponding cached value?

import (
	"context"
	"sync"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/pkg/cache"
)

// Cache holds cached results of calls to Mixer.Check
type Cache struct {
	cache         cache.ExpiringCache
	keyShapes     []keyShape
	keyShapesLock sync.RWMutex
	globalWords   []string

	// allowing patch for testing
	getTime func() time.Time
}

// Value holds the data that the check cache stores.
type Value struct {
	// StatusMessage for the Check operation
	StatusMessage string

	// Expiration is the point at which this cache value becomes stale and shouldn't be used
	Expiration time.Time

	// StatusCode for the Check operation
	StatusCode int32

	// ValidUseCount for the Check operation
	ValidUseCount int32

	// ReferencedAttributes for the Check operation
	ReferencedAttributes mixerpb.ReferencedAttributes

	// RouteDirective for the completed Check operation
	RouteDirective *mixerpb.RouteDirective
}

var (
	// cache stats
	writesTotal = stats.Int64(
		"mixer/checkcache/cache_writes_total", "The number of times state in the cache was added or updated.", stats.UnitDimensionless)
	hitsTotal = stats.Int64(
		"mixer/checkcache/cache_hits_total", "The number of times a cache lookup operation succeeded to find an entry in the cache.", stats.UnitDimensionless)
	missesTotal = stats.Int64(
		"mixer/checkcache/cache_misses_total", "The number of times a cache lookup operation failed to find an entry in the cache.", stats.UnitDimensionless)
	evictionsTotal = stats.Int64(
		"mixer/checkcache/cache_evictions_total", "The number of entries that have been evicted from the cache.", stats.UnitDimensionless)

	writesView    = newView(writesTotal, []tag.Key{}, view.LastValue())
	hitsView      = newView(hitsTotal, []tag.Key{}, view.LastValue())
	missesView    = newView(missesTotal, []tag.Key{}, view.LastValue())
	evictionsView = newView(evictionsTotal, []tag.Key{}, view.LastValue())
)

func newView(measure stats.Measure, keys []tag.Key, aggregation *view.Aggregation) *view.View {
	return &view.View{
		Name:        measure.Name(),
		Description: measure.Description(),
		Measure:     measure,
		TagKeys:     keys,
		Aggregation: aggregation,
	}
}

// New creates a new instance of a check cache with the given maximum capacity. Adding more items to the
// cache then its capacity will cause eviction of older entries.
func New(capacity int32) *Cache {
	cc := &Cache{
		cache:       cache.NewLRU(time.Minute*60, 1*time.Minute, capacity),
		globalWords: attribute.GlobalList(),
		getTime:     time.Now,
	}

	_ = view.Register(writesView, hitsView, missesView, evictionsView)

	return cc
}

// Close releases any resources used by the check cache.
func (cc *Cache) Close() error {
	view.Unregister(writesView, hitsView, missesView, evictionsView)
	return nil
}

// Get looks up an attribute bag in the cache.
func (cc *Cache) Get(attrs attribute.Bag) (Value, bool) {
	cc.keyShapesLock.RLock()
	shapes := cc.keyShapes
	cc.keyShapesLock.RUnlock()

	// find a matching key shape
	for _, shape := range shapes {
		if shape.isCompatible(attrs) {

			// given the compatible key shape, make a key
			key := shape.makeKey(attrs)

			// see if we have an entry in the cache for this key
			if result, ok := cc.cache.Get(key); ok {
				v := result.(Value)
				if v.Expiration.Before(cc.getTime()) {
					// Entry expired. This happens because the underlying ExpiringCache only lazily cleans up
					// expired entries. Since we want to be more precise, we do our own freshness check and throw
					// out stale results
					cc.recordStats()
					return Value{}, false
				}

				// got a match!
				cc.recordStats()
				return result.(Value), true
			}
		}
	}

	cc.recordStats()
	return Value{}, false
}

// Set enters a new value in the cache.
func (cc *Cache) Set(attrs attribute.Bag, value Value) {
	now := cc.getTime()
	if value.Expiration.Before(now) {
		// value is already expired, don't add it
		cc.recordStats()
		return
	}

	cc.keyShapesLock.RLock()
	shapes := cc.keyShapes
	cc.keyShapesLock.RUnlock()

	// find a matching key shape
	for _, shape := range shapes {
		if shape.isCompatible(attrs) {
			cc.cache.SetWithExpiration(shape.makeKey(attrs), value, value.Expiration.Sub(now))
			cc.recordStats()
			return
		}
	}

	shape := newKeyShape(value.ReferencedAttributes, cc.globalWords)

	// Note that there's TOCTOU window here, but it's OK. It doesn't hurt that multiple
	// equivalent keyShape entries may appear in the slice.
	cc.keyShapesLock.Lock()
	cc.keyShapes = append(cc.keyShapes, shape)
	cc.keyShapesLock.Unlock()

	cc.cache.SetWithExpiration(shape.makeKey(attrs), value, value.Expiration.Sub(now))
	cc.recordStats()
}

func (cc *Cache) recordStats() {
	s := cc.cache.Stats()
	stats.Record(context.Background(),
		writesTotal.M(int64(s.Writes)),
		hitsTotal.M(int64(s.Hits)),
		missesTotal.M(int64(s.Misses)),
		evictionsTotal.M(int64(s.Evictions)))
}
