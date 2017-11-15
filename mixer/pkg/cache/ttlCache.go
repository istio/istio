// Copyright 2017 Istio Authors
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
package cache

import (
	"sync"
	"sync/atomic"
	"time"
)

// Due to the use of the time.Time.UnixNano function in this code, expiration
// will fail after the year 2262. Sorry, you'll need to upgrade to a newer version
// of Istio at that time :-)

type ttlCache struct {
	entries           sync.Map
	stats             Stats
	defaultExpiration time.Duration
	evictionTicker    *time.Ticker
	baseTimeNanos     int64
}

// A single cache entry. This is the values we use in our storage map
type entry struct {
	value      interface{}
	expiration int64 // nanoseconds
}

// NewTTL creates a new cache with a time-based eviction model.
//
// Cache eviction is done on a periodic basis. Individual cache entries are evicted
// after their expiration time has passed. The periodic nature of eviction means that
// cache entries tend to survive around (expirationTime + (evictionInterval / 2))
//
// defaultExpiration specifies the default minimum amount of time a cached
// entry remains in the cache before eviction. This value is used with the
// Set function. Explicit per-entry expiration times can be set with the
// SetWithExpiration function instead.
//
// evictionInterval specifies the frequency at which eviction activities take
// place. This should likely be >= 1 second.
//
// Since TTL caches only evict data based on the passage of time, it's possible to
// use up all available memory by continuing to add entries to the cache with a
// long enough expiration time. Don't do that.
func NewTTL(defaultExpiration time.Duration, evictionInterval time.Duration) ExpiringCache {
	c := &ttlCache{
		defaultExpiration: defaultExpiration,
	}

	c.baseTimeNanos = time.Now().UTC().UnixNano()

	if evictionInterval > 0 {
		c.evictionTicker = time.NewTicker(evictionInterval)
		go c.evicter()
	}

	return c
}

func (c ttlCache) Close() error {
	if c.evictionTicker != nil {
		c.evictionTicker.Stop()
	}

	return nil
}

func (c *ttlCache) evicter() {
	// Wake up once in a while and evict stale items
	for now := range c.evictionTicker.C {
		c.evictExpired(now)
	}
}

func (c *ttlCache) evictExpired(t time.Time) {
	// We snapshot a base time here such that the time doesn't need to be
	// sampled in the Set call as calling time.Now() is relatively expensive.
	// Doing it here provides enough precision for our needs and tends to have
	// much lower call frequency.
	n := t.UTC().UnixNano()
	atomic.StoreInt64(&c.baseTimeNanos, n)

	var count uint64

	c.entries.Range(func(key interface{}, value interface{}) bool {
		e := value.(*entry)
		if e.expiration <= n {
			c.entries.Delete(key)
			count++
		}
		return true
	})

	atomic.AddUint64(&c.stats.Writes, count)
}

func (c *ttlCache) Set(key interface{}, value interface{}) {
	c.SetWithExpiration(key, value, c.defaultExpiration)
}

func (c *ttlCache) SetWithExpiration(key interface{}, value interface{}, expiration time.Duration) {
	e := &entry{
		value:      value,
		expiration: atomic.LoadInt64(&c.baseTimeNanos) + expiration.Nanoseconds(),
	}

	c.entries.Store(key, e)
	atomic.AddUint64(&c.stats.Writes, 1)
}

func (c *ttlCache) Get(key interface{}) (interface{}, bool) {
	e, ok := c.entries.Load(key)
	if !ok {
		atomic.AddUint64(&c.stats.Misses, 1)
		return nil, false
	}

	// Note that we could check the current time here and discard the returned value
	// if the expiration time has passed. But this would increase this function's execution
	// time by > 50% (since time.Now is relatively expensive). Instead, we don't check time
	// here and accept some imprecision in actual eviction times.

	atomic.AddUint64(&c.stats.Hits, 1)
	return e.(*entry).value, true
}

func (c *ttlCache) Remove(key interface{}) {
	c.entries.Delete(key)
	atomic.AddUint64(&c.stats.Writes, 1)
}

func (c *ttlCache) Stats() Stats {
	return c.stats
}
