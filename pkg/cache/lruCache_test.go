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
	"testing"
	"time"
)

func TestLRUBasic(t *testing.T) {
	lru := NewLRU(5*time.Minute, 1*time.Millisecond, 500)
	testCacheBasic(lru, t)
}

func TestLRUConcurrent(t *testing.T) {
	lru := NewLRU(5*time.Minute, 1*time.Minute, 500)
	testCacheConcurrent(lru, t)
}

func TestLRUExpiration(t *testing.T) {
	lru := NewLRU(5*time.Second, 0, 500).(*lruCache)
	testCacheExpiration(lru, lru.evictExpired, t)
}

func TestLRUEvicter(t *testing.T) {
	lru := NewLRU(5*time.Second, 1*time.Millisecond, 500)
	testCacheEvicter(lru)
}

func TestLRUEvictExpired(t *testing.T) {
	lru := NewLRU(5*time.Second, 0, 500).(*lruCache)
	testCacheEvictExpired(lru, t)
}

func TestLRUFinalizer(t *testing.T) {
	lru := NewLRU(5*time.Second, 1*time.Millisecond, 500).(*lruWrapper)
	testCacheFinalizer(&lru.evicterTerminated)
}

func TestLRUBehavior(t *testing.T) {
	lru := NewLRU(5*time.Minute, 1*time.Millisecond, 3)

	lru.Set("1", "1")
	lru.Set("2", "2")
	lru.Set("3", "3")
	lru.Set("4", "4")

	// make sure only the expected entries are there
	_, ok1 := lru.Get("1")
	_, ok2 := lru.Get("2")
	_, ok3 := lru.Get("3")
	_, ok4 := lru.Get("4")
	if ok1 || !ok2 || !ok3 || !ok4 {
		t.Errorf("Got %v %v %v %v, expected false, true, true, true", ok1, ok2, ok3, ok4)
	}

	// make "2" the MRU
	_, _ = lru.Get("2")

	// push something new in and make sure "2" is still there
	lru.Set("5", "5")

	_, ok1 = lru.Get("1")
	_, ok2 = lru.Get("2")
	_, ok3 = lru.Get("3")
	_, ok4 = lru.Get("4")
	_, ok5 := lru.Get("5")
	if ok1 || !ok2 || ok3 || !ok4 || !ok5 {
		t.Errorf("Got %v %v %v %v %v, expected false, true, false, true, true", ok1, ok2, ok3, ok4, ok5)
	}
}

func BenchmarkLRUGet(b *testing.B) {
	c := NewLRU(5*time.Minute, 1*time.Minute, 500)
	benchmarkCacheGet(c, b)
}

func BenchmarkLRUGetConcurrent(b *testing.B) {
	c := NewLRU(5*time.Minute, 1*time.Minute, 500)
	benchmarkCacheGetConcurrent(c, b)
}

func BenchmarkLRUSet(b *testing.B) {
	c := NewLRU(5*time.Minute, 1*time.Minute, 500)
	benchmarkCacheSet(c, b)
}

func BenchmarkLRUSetConcurrent(b *testing.B) {
	c := NewLRU(5*time.Minute, 1*time.Minute, 500)
	benchmarkCacheSetConcurrent(c, b)
}

func BenchmarkLRUGetSetConcurrent(b *testing.B) {
	c := NewLRU(5*time.Minute, 1*time.Minute, 500)
	benchmarkCacheGetSetConcurrent(c, b)
}

func BenchmarkLRUSetRemove(b *testing.B) {
	c := NewLRU(5*time.Minute, 1*time.Minute, 500)
	benchmarkCacheSetRemove(c, b)
}
