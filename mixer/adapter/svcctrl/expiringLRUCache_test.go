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
		t.Errorf(`expiringLRUCache.add(1, 1) != false`)
	}
	// cache contents: {(1, 2)}
	if evicted := cache.add(1, 2); evicted {
		t.Errorf(`expiringLRUCache.add(1, 2) != false`)
	}
	if value, ok := cache.get(1); !ok || value != 2 {
		t.Errorf(`expiringLRUCache.get(1) != 2, true`)
	}
	// cache contents: {(1,2), (2,3)}
	if evicted := cache.add(2, 3); evicted {
		t.Errorf(`expiringLRUCache.add(2, 3) != false`)
	}
	// expiringLRUCache {(2,3}, (3,4)}
	if evicted := cache.add(3, 4); !evicted {
		t.Errorf(`expiringLRUCache.add(3, 4) != true`)
	}
	if value, ok := cache.get(1); ok || value != nil {
		t.Errorf(`expiringLRUCache.get(1) != nil, false`)
	}
	// expiringLRUCache {(3,4), (2,3)}
	if value, ok := cache.get(2); !ok && (value == nil || value != 3) {
		t.Errorf(`expiringLRUCache.get(2) != 3, true`)
	}
	// expiringLRUCache {(2,3), (4,5)}
	if evicted := cache.add(4, 5); !evicted {
		t.Errorf(`expiringLRUCache.add(4,5) != true`)
	}
	if value, ok := cache.get(2); !ok || (value == nil || value != 3) {
		t.Errorf(`expiringLRUCache.get(2) != 3, true`)
	}
}

func TestExpiration(t *testing.T) {
	fakeClock := clockwork.NewFakeClock()
	cache := newTestCache(2, time.Second, fakeClock, t)
	// cache contents: {(1, 1)}
	if evicted := cache.add(1, 1); evicted {
		t.Errorf(`expiringLRUCache.add(1, 1) != false`)
	}
	fakeClock.Advance(time.Second * 2)
	if value, ok := cache.get(1); ok || value != nil {
		t.Errorf(`expiringLRUCache.get(1) != nil, false`)
	}
}

func TestRemove(t *testing.T) {
	fakeClock := clockwork.NewFakeClock()
	cache := newTestCache(2, time.Second, fakeClock, t)
	// cache contents: {(1, 1)}
	if evicted := cache.add(1, 1); evicted {
		t.Errorf(`expiringLRUCache.add(1, 1) != false`)
	}
	if ok := cache.remove(0); ok {
		t.Errorf(`expiringLRUCache.remove(0) != false`)
	}
	if ok := cache.remove(1); !ok {
		t.Errorf(`expiringLRUCache.remove(0) != true`)
	}
}

func TestPurge(t *testing.T) {
	fakeClock := clockwork.NewFakeClock()
	cache := newTestCache(2, time.Second, fakeClock, t)
	// cache contents: {(1, 1)}
	if evicted := cache.add(1, 1); evicted {
		t.Errorf(`expiringLRUCache.add(1, 1) != false`)
	}
	// cache contents: {(1, 1), {2, 3}
	if evicted := cache.add(2, 3); evicted {
		t.Errorf(`expiringLRUCache.add(2, 3) != false`)
	}
	cache.purge()
	if value, ok := cache.get(2); ok || value != nil {
		t.Errorf(`expiringLRUCache.get(2) != nil, false`)
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
