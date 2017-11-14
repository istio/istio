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
	"testing"
	"time"

	"github.com/hashicorp/golang-lru/simplelru"
	"github.com/jonboulle/clockwork"
)

func TestAddAndGet(t *testing.T) {
	fakeClock := clockwork.NewFakeClock()
	cache := newTestCache(2, time.Second, fakeClock, t)
	// cache contents: {(1, 1)}
	if evicted := cache.add(1, 1); evicted {
		t.Errorf(`add(1, 1), expect no eviction but evicted`)
	}
	// cache contents: {(1, 2)}
	if evicted := cache.add(1, 2); evicted {
		t.Errorf(`add(1, 2), expect no eviction but evicted`)
	}
	if value, ok := cache.get(1); !ok || value != 2 {
		t.Errorf(`expect (1, 2) in cache, but not found`)
	}
	// cache contents: {(1,2), (2,3)}
	if evicted := cache.add(2, 3); evicted {
		t.Errorf(`add(2, 3), expect no eviction, but evicted `)
	}
	// expiringLRUCache {(2,3}, (3,4)}
	if evicted := cache.add(3, 4); !evicted {
		t.Errorf(`add(3, 4), expect eviction, but not evicted`)
	}
	if value, ok := cache.get(1); ok || value != nil {
		t.Errorf(`expect key 1 evicted, but found in cache`)
	}
	// expiringLRUCache {(3,4), (2,3)}
	if value, ok := cache.get(2); !ok && (value == nil || value != 3) {
		t.Errorf(`expect (2, 3) in cache, but not found`)
	}
	// expiringLRUCache {(2,3), (4,5)}
	if evicted := cache.add(4, 5); !evicted {
		t.Errorf(`add(4, 5), expect eviction, but not evicted`)
	}
	if value, ok := cache.get(2); !ok || (value == nil || value != 3) {
		t.Errorf(`expect (2, 3) in cache, but not found`)
	}
}

func TestExpiration(t *testing.T) {
	fakeClock := clockwork.NewFakeClock()
	cache := newTestCache(2, time.Second, fakeClock, t)
	// cache contents: {(1, 1)}
	if evicted := cache.add(1, 1); evicted {
		t.Errorf(`add(1, 1), expect no eviction, but evicted`)
	}
	fakeClock.Advance(time.Second + time.Nanosecond)
	if value, ok := cache.get(1); ok || value != nil {
		t.Errorf(`expect key 1 expired, but still found in cache`)
	}
}

func TestRemove(t *testing.T) {
	fakeClock := clockwork.NewFakeClock()
	cache := newTestCache(2, time.Second, fakeClock, t)
	// cache contents: {(1, 1)}
	if evicted := cache.add(1, 1); evicted {
		t.Errorf(`add(1, 1), expect no eviction, but evicted`)
	}
	if ok := cache.remove(0); ok {
		t.Errorf(`expect remove(0) to return false'`)
	}
	if _, ok := cache.get(1); !ok {
		t.Errorf(`expect key 1 in cache, but not found`)
	}
	// cache contents: {}
	if ok := cache.remove(1); !ok {
		t.Errorf(`expect to remove existing key 1, but report key 1 not found`)
	}
	if _, ok := cache.get(1); ok {
		t.Errorf(`key 1 should have been removed`)
	}
}

func TestPurge(t *testing.T) {
	fakeClock := clockwork.NewFakeClock()
	cache := newTestCache(2, time.Second, fakeClock, t)
	// cache contents: {(1, 1)}
	if evicted := cache.add(1, 1); evicted {
		t.Errorf(`add(1, 1), expect no eviction, but evicted`)
	}
	// cache contents: {(1, 1), {2, 3}
	if evicted := cache.add(2, 3); evicted {
		t.Errorf(`add(2, 3), expect no eviction, but evicted`)
	}
	cache.purge()
	if value, ok := cache.get(1); ok || value != nil {
		t.Errorf(`expect empty cache, but key 1 exists`)
	}
	if value, ok := cache.get(2); ok || value != nil {
		t.Errorf(`expect empty cache, but key 2 exists`)
	}
}

func TestNewExpiringLRUCache(t *testing.T) {
	cache, err := newExpiringLRUCache(2, time.Second*10)
	if err != nil {
		t.Fatal("fail to create expiringLRUCache")
	}
	_ = cache.add(1, 2)
	if value, ok := cache.get(1); !ok || value == nil || value != 2 {
		t.Errorf("fail to add and get item to cache")
	}
}

func newTestCache(size int, expiration time.Duration, clock clockwork.Clock, t *testing.T) *expiringLRUCache {
	lru, err := simplelru.NewLRU(size, nil)
	if err != nil {
		t.Fatal("fail to create simplelru cache")
	}

	return &expiringLRUCache{
		lru:        lru,
		clock:      clock,
		expiration: expiration,
	}
}
