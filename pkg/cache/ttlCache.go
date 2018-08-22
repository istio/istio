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
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Due to the use of the time.Time.UnixNano function in this code, expiration
// will fail after the year 2262. Sorry, you'll need to upgrade to a newer version
// of Istio at that time :-)
//
// This code does some trickery with finalizers in order to avoid the need for a Close
// method. Given the nature of this code, forgetting to call Close on one of these objects
// can lead to a substantial permanent memory leak in a process by causing the cache to
// remain alive forever, along with all the entries the cache points to. The use of the
// ttlWrapper type makes it so we control the exposure of the underlying ttlCache pointer.
// When the pointer to ttlWrapper is finalized, this tells us to go ahead and stop the
// evicter goroutine, which allows the lruCache instance to be collected and everything
// ends well.

// See use of SetFinalizer below for an explanation of this weird composition
type ttlWrapper struct {
	*ttlCache
}

type ttlCache struct {
	entries           sync.Map
	stats             Stats
	defaultExpiration time.Duration
	stopEvicter       chan bool
	baseTimeNanos     int64
	evicterTerminated sync.WaitGroup // used by unit tests to verify the finalizer ran
	callback          EvictionCallback
}

// A single cache entry. This is the values we use in our storage map
type entry struct {
	value      interface{}
	expiration int64 // nanoseconds
}

// EvictionCallback is a function that will be called on entry eviction
// from an ExpiringCache.
//
// This callback will be invoked immediately after the entry is deleted
// from the `sync.Map` that backs this cache (using `Map.Delete()`). No
// locks are held during the invocation of this callback. The callback
// should not result in blocking calls to long-running operations, however.
type EvictionCallback func(key, value interface{})

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
	return NewTTLWithCallback(defaultExpiration, evictionInterval, func(key, value interface{}) {})
}

// NewTTLWithCallback creates a new cache with a time-based eviction model that will invoke the supplied
// callback on all evictions. See also: NewTTL.
func NewTTLWithCallback(defaultExpiration time.Duration, evictionInterval time.Duration, callback EvictionCallback) ExpiringCache {
	c := &ttlCache{
		defaultExpiration: defaultExpiration,
		callback:          callback,
	}

	if evictionInterval > 0 {
		c.baseTimeNanos = time.Now().UTC().UnixNano()
		c.stopEvicter = make(chan bool, 1)
		c.evicterTerminated.Add(1)
		go c.evicter(evictionInterval)

		// We return a 'see-through' wrapper for the real object such that
		// the finalizer can trigger on the wrapper. We can't set a finalizer
		// on the main cache object because it would never fire, since the
		// evicter goroutine is keeping it alive
		result := &ttlWrapper{c}
		runtime.SetFinalizer(result, func(w *ttlWrapper) {
			w.stopEvicter <- true
			w.evicterTerminated.Wait()
		})
		return result
	}

	return c
}

func (c *ttlCache) evicter(evictionInterval time.Duration) {
	// Wake up once in a while and evict stale items
	ticker := time.NewTicker(evictionInterval)
	for {
		select {
		case now := <-ticker.C:
			c.evictExpired(now)
		case <-c.stopEvicter:
			ticker.Stop()
			c.evicterTerminated.Done() // record this for the sake of unit tests
			return
		}
	}
}

func (c *ttlCache) evictExpired(t time.Time) {
	// We snapshot a base time here such that the time doesn't need to be
	// sampled in the Set call as calling time.Now() is relatively expensive.
	// Doing it here provides enough precision for our needs and tends to have
	// much lower call frequency.
	n := t.UTC().UnixNano()
	atomic.StoreInt64(&c.baseTimeNanos, n)

	// This loop is inherently racy. As we iterate through the
	// key/value pairs, the value assigned to a particular key may
	// change at any point. So when we find an expired entry and
	// delete it, it's possible that a concurrent update assigned a
	// fresh value to the key at hand, and so we'll proceed to delete
	// the fresh key/value combo.
	//
	// This is a cache, not a map. So we're OK with this extremely rare
	// situation. So long as the cache never lies, it's OK if it spuriously
	// forgets.
	c.entries.Range(func(key interface{}, value interface{}) bool {
		e := value.(*entry)
		if e.expiration <= n {
			c.entries.Delete(key)
			c.callback(key, value.(*entry).value)
			// Note: can miscount if the key was removed before it was evicted
			atomic.AddUint64(&c.stats.Evictions, 1)
		}
		return true
	})
}

func (c *ttlCache) EvictExpired() {
	c.evictExpired(time.Now())
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

	// Note: we count this as a removal even in the case where the key wasn't actually in the map
	atomic.AddUint64(&c.stats.Removals, 1)
}

func (c *ttlCache) RemoveAll() {
	c.entries.Range(func(key interface{}, value interface{}) bool {
		c.entries.Delete(key)

		// Note: can miscount if the key was evicted before it was removed
		atomic.AddUint64(&c.stats.Removals, 1)

		return true
	})
}

func (c *ttlCache) Stats() Stats {
	return Stats{
		Evictions: atomic.LoadUint64(&c.stats.Evictions),
		Hits:      atomic.LoadUint64(&c.stats.Hits),
		Misses:    atomic.LoadUint64(&c.stats.Misses),
		Writes:    atomic.LoadUint64(&c.stats.Writes),
		Removals:  atomic.LoadUint64(&c.stats.Removals),
	}
}
