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
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// We keep a slice of entries representing all items currently in the cache.
// The entries in this slice are organized in a doubly-linked circular list. The list
// is created via next/prev fields that contain indices into the slice. Using a slice
// with indices in this way is considerably more efficient, in both time and space, than
// it would be to allocate distinct objects for each cache entry and use pointers to
// link them.
//
// The first entry in the slice is a sentinel entry and serves as anchor and is both the
// beginning and end of the list. This arrangement makes it so insertion and removal
// from the list can be done without conditionals. The sentinel's prev field holds the
// index of the last entry in the list, while the sentinel's next field holds the index of
// the first entry in the list.
//
// The LRU algorithm we currently use is classically simple. Both getting and setting
// a cache entry puts that entry at the head of the LRU list. When we need to make
// room in the cache, we always just plop the current tail from the list.
//
// Once this code has been in active use for a while in a real system, we
// should evaluate whether fancier LRU regimes would improve overall perf.
// For example, we could merely bump entries up in the list by one slot per
// use instead of putting them at the head of the list. Or we could require
// N accesses before promoting an entry to the head. Etc.
//
// If we do get more sophisticated, we could consider introducing an explicit MRU section
// of the list (MRU=Most Recently Used). Entries in that portion of the list wouldn't be shuffled
// around upon use, they would be treated as a whole distinct class. The idea is that for frequently
// used entries, we can avoid messing up processor caches by moving these entries and can use
// read/write locks to increase scalability.
//
// Another idea to improve scalability is to turn the Get call into a read-only operation and
// defer updating the LRU ordering to a background goroutine. Done right, this could allow
// many concurrent readers and could trivially bring some of the effects of the MRU design by
// 'slowing down' the rate of mutation to the list, eliminating the 'jitter' of frequently used
// entries. The challenge with this approach is coordinating the code that runs under lock and the
// async code that doesn't.
//
// Due to the use of the time.Time.UnixNano function in this code, expiration
// will fail after the year 2262. Sorry, you'll need to upgrade to a newer version
// of Istio at that time :-)
//
// This code does some trickery with finalizers in order to avoid the need for a Close
// method. Given the nature of this code, forgetting to call Close on one of these objects
// can lead to a substantial permanent memory leak in a process by causing the cache to
// remain alive forever, along with all the entries the cache points to. The use of the
// lruWrapper type makes it so we control the exposure of the underlying lruCache pointer.
// When the pointer to lruWrapper is finalized, this tells us to go ahead and stop the
// evicter goroutine, which allows the lruCache instance to be collected and everything
// ends well.
//
// A potential idea for the future would be to defer LRU ordering changes from being
// done inline in Get and instead being deferred to a background goroutine. This could
// allow Get to be read-only which would allow more concurrency.

// See use of SetFinalizer below for an explanation of this weird composition
type lruWrapper struct {
	*lruCache
}

type lruCache struct {
	sync.RWMutex
	entries           []lruEntry            // allocate once, not resizable
	sentinel          *lruEntry             // direct pointer to entries[0] to avoid bounds checking
	lookup            map[interface{}]int32 // keys => entry index
	stats             Stats
	defaultExpiration time.Duration
	stopEvicter       chan bool
	baseTimeNanos     int64
	evicterTerminated sync.WaitGroup // used by unit tests to verify the finalizer ran
}

// lruEntry is used to hold a value in the ordered lru list represented by the entry slice
type lruEntry struct {
	next       int32       // index of next entry
	prev       int32       // index of previous entry
	key        interface{} // cache key associated with this entry
	value      interface{} // cache value associated with this entry
	expiration int64       // nanoseconds
}

// entry 0 in the slice is the sentinel node
const sentinelIndex = 0

// NewLRU creates a new cache with an LRU and time-based eviction model.
//
// Cache eviction is done on a periodic basis. Individual cache entries are evicted
// after their expiration time has passed. The periodic nature of eviction means that
// cache entries tend to survive around (expirationTime + (evictionInterval / 2))
//
// In addition, when the cache is full, adding a new item will displace the item that has
// been referenced least recently.
//
// defaultExpiration specifies the default minimum amount of time a cached
// entry remains in the cache before eviction. This value is used with the
// Set function. Explicit per-entry expiration times can be set with the
// SetWithExpiration function instead.
//
// evictionInterval specifies the frequency at which eviction activities take
// place. This should likely be >= 1 second.
func NewLRU(defaultExpiration time.Duration, evictionInterval time.Duration, maxEntries int32) ExpiringCache {
	c := &lruCache{
		entries:           make([]lruEntry, maxEntries+1),
		lookup:            make(map[interface{}]int32, maxEntries),
		defaultExpiration: defaultExpiration,
	}

	// create the linked list of entries
	for i := int32(0); i < maxEntries+1; i++ {
		c.entries[i].next = i + 1
		c.entries[i].prev = i - 1
		c.entries[i].expiration = math.MaxInt64
	}
	c.sentinel = &c.entries[0]

	// finish things off, making the list circular
	c.entries[maxEntries].next = sentinelIndex
	c.sentinel.prev = maxEntries

	if evictionInterval > 0 {
		c.baseTimeNanos = time.Now().UTC().UnixNano()
		c.stopEvicter = make(chan bool, 1)
		c.evicterTerminated.Add(1)
		go c.evicter(evictionInterval)

		// We return a 'see-through' wrapper for the real object such that
		// the finalizer can trigger on the wrapper. We can't set a finalizer
		// on the main cache object because it would never fire, since the
		// evicter goroutine is keeping it alive
		result := &lruWrapper{c}
		runtime.SetFinalizer(result, func(w *lruWrapper) {
			w.stopEvicter <- true
			w.evicterTerminated.Wait()
		})
		return result
	}

	return c
}

func (c *lruCache) evicter(evictionInterval time.Duration) {
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

func (c *lruCache) evictExpired(t time.Time) {
	// We snapshot a base time here such that the time doesn't need to be
	// sampled in the Set call as calling time.Now() is relatively expensive.
	// Doing it here provides enough precision for our needs and tends to have
	// much lower call frequency.
	n := t.UTC().UnixNano()
	atomic.StoreInt64(&c.baseTimeNanos, n)

	for i := int32(1); i < int32(len(c.entries)); i++ {
		ent := &c.entries[i]

		c.Lock()
		if ent.expiration <= n {
			c.remove(i)
			c.stats.Evictions++
		}
		c.Unlock()
	}
}

func (c *lruCache) EvictExpired() {
	c.evictExpired(time.Now())
}

func (c *lruCache) unlinkEntry(index int32) {
	ent := &c.entries[index]

	c.entries[ent.prev].next = ent.next
	c.entries[ent.next].prev = ent.prev
}

func (c *lruCache) linkEntryAtHead(index int32) {
	ent := &c.entries[index]

	ent.next = c.sentinel.next
	ent.prev = sentinelIndex
	c.entries[ent.next].prev = index
	c.sentinel.next = index
}

func (c *lruCache) linkEntryAtTail(index int32) {
	ent := &c.entries[index]

	ent.next = sentinelIndex
	ent.prev = c.sentinel.prev
	c.entries[ent.prev].next = index
	c.sentinel.prev = index
}

func (c *lruCache) Set(key interface{}, value interface{}) {
	c.SetWithExpiration(key, value, c.defaultExpiration)
}

func (c *lruCache) SetWithExpiration(key interface{}, value interface{}, expiration time.Duration) {
	exp := atomic.LoadInt64(&c.baseTimeNanos) + expiration.Nanoseconds()

	c.Lock()

	index, ok := c.lookup[key]
	if !ok {
		// reclaim the tail entry
		index = c.sentinel.prev
		delete(c.lookup, c.entries[index].key)
		c.lookup[key] = index
	}

	c.unlinkEntry(index)
	c.linkEntryAtHead(index)
	ent := &c.entries[index]
	ent.key = key
	ent.value = value
	ent.expiration = exp

	atomic.AddUint64(&c.stats.Writes, 1)

	c.Unlock()
}

func (c *lruCache) Get(key interface{}) (interface{}, bool) {
	c.Lock()

	var value interface{}
	index, ok := c.lookup[key]
	if ok {
		c.unlinkEntry(index)
		c.linkEntryAtHead(index)
		value = c.entries[index].value
		c.stats.Hits++
	} else {
		c.stats.Misses++
	}

	c.Unlock()

	return value, ok
}

func (c *lruCache) Remove(key interface{}) {
	c.Lock()

	if index, ok := c.lookup[key]; ok {
		c.remove(index)
		c.stats.Removals++
	}

	c.Unlock()
}

func (c *lruCache) RemoveAll() {
	for i := 1; i < len(c.entries); i++ {
		ent := &c.entries[i]

		c.Lock()
		if ent.key != nil {
			c.remove(int32(i))
			c.stats.Removals++
		}
		c.Unlock()
	}
}

func (c *lruCache) remove(index int32) {
	ent := &c.entries[index]

	delete(c.lookup, ent.key)
	c.unlinkEntry(index)
	c.linkEntryAtTail(index)
	ent.key = nil
	ent.value = nil
	ent.expiration = math.MaxInt64
}

func (c *lruCache) Stats() Stats {
	c.RLock()
	defer c.RUnlock()
	return c.stats
}

/* debugging aid
func (c *lruCache) dumpList(banner string) {
	fmt.Printf("%s\n", banner)
	index := c.entries[0].next
	count := 8
	for {
		fmt.Printf("  %d: prev %d, next %d, payload {%v:%v}\n",
			index, c.entries[index].prev, c.entries[index].next, c.entries[index].key, c.entries[index].value)

		count--
		if count == 0 {
			break
		}

		index = c.entries[index].next
		if index == 0 {
			break
		}
	}
	fmt.Println()
}
*/
