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

package svcctrl

import (
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/simplelru"
	"github.com/jonboulle/clockwork"
)

type (
	cacheValue struct {
		// insert timestamp
		timestamp time.Time
		value     interface{}
	}

	// A thread safe LRU cache with expiration.
	expiringLRUCache struct {
		mutex sync.RWMutex
		lru   *simplelru.LRU
		clock clockwork.Clock
		// Items that stay in cache longer than expiration will be evicted.
		expiration time.Duration
	}
)

// add adds a key-value pair in cache and returns true if an eviction occurred. Overwriting an
// existing key is not an eviction.
func (c *expiringLRUCache) add(key, value interface{}) bool {
	now := c.clock.Now()
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.lru.Add(key, &cacheValue{
		now,
		value,
	})
}

// get gets the cached item of the given key. Expired item will be evicted. Returns false
// if item doesn't exist or expired.
func (c *expiringLRUCache) get(key interface{}) (interface{}, bool) {
	value := c.getImpl(key)
	if value == nil {
		return nil, false
	}

	if c.clock.Since(value.timestamp) <= c.expiration {
		return value.value, true
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()
	tmp, ok := c.lru.Get(key)
	if !ok {
		return nil, false
	}

	valueInCache := tmp.(*cacheValue)
	if c.clock.Since(valueInCache.timestamp) > c.expiration {
		c.lru.Remove(key)
		return nil, false
	}
	return valueInCache.value, true
}

// remove removes the item of the given key and returns true if the item was in cache.
func (c *expiringLRUCache) remove(key interface{}) bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.lru.Remove(key)
}

// purge purges all items in the cache.
func (c *expiringLRUCache) purge() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.lru.Purge()
}

// getImpl gets the item of given key and returns nil if item doesn't exist.
func (c *expiringLRUCache) getImpl(key interface{}) *cacheValue {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	value, ok := c.lru.Get(key)
	if !ok {
		return nil
	}
	return value.(*cacheValue)
}

// newExpiringLRUCache creates a cache of given size and expiration.
func newExpiringLRUCache(size int, expiration time.Duration) (*expiringLRUCache, error) {
	lru, err := simplelru.NewLRU(size, nil)
	if err != nil {
		return nil, err
	}

	return &expiringLRUCache{
		lru:        lru,
		clock:      clockwork.NewRealClock(),
		expiration: expiration,
	}, nil
}
