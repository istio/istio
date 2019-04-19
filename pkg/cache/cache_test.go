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
	"strconv"
	"sync"
	"testing"
	"time"
)

type cacheOp int

const (
	Get = iota
	Set
	Remove
	RemoveAll
)

func testCacheBasic(c Cache, t *testing.T) {
	cases := []struct {
		op     cacheOp
		key    string
		value  string
		result bool
		stats  Stats
	}{
		// try to get when the entry isn't present
		{Get, "X", "", false, Stats{Misses: 1}},

		// add an entry and make sure we can get it
		{Set, "X", "12", false, Stats{Misses: 1, Writes: 1}},
		{Get, "X", "12", true, Stats{Misses: 1, Writes: 1, Hits: 1}},
		{Get, "X", "12", true, Stats{Misses: 1, Writes: 1, Hits: 2}},

		// check interference between get/set
		{Get, "Y", "", false, Stats{Misses: 2, Writes: 1, Hits: 2}},
		{Set, "X", "23", false, Stats{Misses: 2, Writes: 2, Hits: 2}},
		{Get, "X", "23", true, Stats{Misses: 2, Writes: 2, Hits: 3}},
		{Set, "Y", "34", false, Stats{Misses: 2, Writes: 3, Hits: 3}},
		{Get, "X", "23", true, Stats{Misses: 2, Writes: 3, Hits: 4}},
		{Get, "Y", "34", true, Stats{Misses: 2, Writes: 3, Hits: 5}},

		// ensure removing X works and doesn't affect Y
		{Remove, "X", "", false, Stats{Misses: 2, Writes: 3, Hits: 5}},
		{Get, "X", "", false, Stats{Misses: 3, Writes: 3, Hits: 5}},
		{Get, "Y", "34", true, Stats{Misses: 3, Writes: 3, Hits: 6}},

		// make sure everything recovers from remove and then get/set
		{Remove, "X", "", false, Stats{Misses: 3, Writes: 3, Hits: 6}},
		{Remove, "Y", "", false, Stats{Misses: 3, Writes: 3, Hits: 6}},
		{Get, "Y", "", false, Stats{Misses: 4, Writes: 3, Hits: 6}},
		{Set, "X", "45", false, Stats{Misses: 4, Writes: 4, Hits: 6}},
		{Get, "X", "45", true, Stats{Misses: 4, Writes: 4, Hits: 7}},
		{Get, "Y", "", false, Stats{Misses: 5, Writes: 4, Hits: 7}},

		// remove a missing entry, should be a nop
		{Remove, "Z", "", false, Stats{Misses: 5, Writes: 4, Hits: 7}},

		// remove everything
		{Set, "A", "45", false, Stats{Misses: 5, Writes: 5, Hits: 7}},
		{Set, "B", "45", false, Stats{Misses: 5, Writes: 6, Hits: 7}},
		{RemoveAll, "", "", false, Stats{Misses: 5, Writes: 6, Hits: 7}},
		{Get, "A", "45", false, Stats{Misses: 6, Writes: 6, Hits: 7}},
		{Get, "B", "45", false, Stats{Misses: 7, Writes: 6, Hits: 7}},
	}

	for i, tc := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			switch tc.op {
			case Get:
				value, result := c.Get(tc.key)

				if result != tc.result {
					t.Errorf("Got result %v, expected %v", result, tc.result)
				}

				if result {
					str := value.(string)

					if str != tc.value {
						t.Errorf("Got value %v, expected %v", str, tc.value)
					}
				} else if value != nil {
					t.Errorf("Got value %v, expected nil", value)
				}

			case Set:
				c.Set(tc.key, tc.value)

			case Remove:
				c.Remove(tc.key)

			case RemoveAll:
				c.RemoveAll()
			}

			s := c.Stats()

			// removals are inconsistently tracked between implementations, so we ignore these here
			s.Removals = 0

			if s != tc.stats {
				t.Errorf("Got stats of %v, expected %v", s, tc.stats)
			}
		})
	}
}

func testCacheConcurrent(c Cache, t *testing.T) {
	wg := new(sync.WaitGroup)
	workers := runtime.NumCPU()
	wg.Add(workers)

	const numIters = 100000
	for i := 0; i < workers; i++ {
		workerNum := i
		go func() {
			for j := 0; j < numIters; j++ {

				key := "X" + strconv.Itoa(workerNum) + "." + strconv.Itoa(workerNum)
				c.Set(key, j)
				v, ok := c.Get(key)
				if !ok {
					t.Errorf("Got false for key %s, expecting true", key)
				} else if v.(int) != j {
					t.Errorf("Got %d for key %s, expecting %d", v, key, j)
				}
				c.Remove(key)
			}
			wg.Done()
		}()
	}
	wg.Wait()

	stats := c.Stats()
	if stats.Misses != 0 {
		t.Errorf("Got %d misses, expecting %d", stats.Misses, 0)
	}

	if stats.Hits != uint64(workers*numIters) {
		t.Errorf("Got %d hits, expecting %d", stats.Hits, workers*numIters)
	}

	if stats.Writes != uint64(workers*numIters) {
		t.Errorf("Got %d writes, expecting %d", stats.Writes, workers*numIters*2)
	}

}

// WARNING: This test expects the cache to have been created with no automatic eviction.
func testCacheExpiration(c ExpiringCache, evictExpired func(time.Time), t *testing.T) {
	now := time.Now()

	c.SetWithExpiration("EARLY", "123", 10*time.Millisecond)
	c.SetWithExpiration("LATER", "123", 20*time.Millisecond+123*time.Nanosecond)

	evictExpired(now)
	s := c.Stats()

	_, ok := c.Get("EARLY")
	if !ok {
		t.Errorf("Got no value, expected EARLY to be present")
	}

	_, ok = c.Get("LATER")
	if !ok {
		t.Errorf("Got no value, expected LATER to be present")
	}

	if s.Evictions != 0 {
		t.Errorf("Got %d evictions, expecting 0", s.Evictions)
	}

	evictExpired(now.Add(15 * time.Millisecond))
	s = c.Stats()

	_, ok = c.Get("EARLY")
	if ok {
		t.Errorf("Got value, expected EARLY to have been evicted")
	}

	_, ok = c.Get("LATER")
	if !ok {
		t.Errorf("Got no value, expected LATER to still be present")
	}

	if s.Evictions != 1 {
		t.Errorf("Got %d evictions, expecting 1", s.Evictions)
	}

	evictExpired(now.Add(25 * time.Millisecond))
	s = c.Stats()

	_, ok = c.Get("EARLY")
	if ok {
		t.Errorf("Got value, expected EARLY to have been evicted")
	}

	_, ok = c.Get("LATER")
	if ok {
		t.Errorf("Got value, expected LATER to have been evicted")
	}

	if s.Evictions != 2 {
		t.Errorf("Got %d evictions, expecting 2", s.Evictions)
	}
}

func testCacheEvictExpired(c ExpiringCache, t *testing.T) {
	c.SetWithExpiration("A", "A", 1*time.Millisecond)

	_, ok := c.Get("A")
	if !ok {
		t.Error("Got no entry, expecting it to be there")
	}

	time.Sleep(10 * time.Millisecond)
	c.EvictExpired()

	_, ok = c.Get("A")
	if ok {
		t.Error("Got an entry, expecting it to have been evicted")
	}
}

func testCacheEvicter(c ExpiringCache) {
	c.SetWithExpiration("A", "A", 1*time.Millisecond)

	// loop until eviction happens. If eviction doesn't happen, this loop will get stuck forever which is fine
	for {
		time.Sleep(10 * time.Millisecond)

		_, ok := c.Get("A")
		if !ok {
			// item disappeared, we're done
			return
		}
	}
}

func testCacheFinalizer(gate *sync.WaitGroup) {
	runtime.GC()
	gate.Wait()
}

func benchmarkCacheGet(c Cache, b *testing.B) {
	c.Set("foo", "bar")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Get("foo")
	}
}

func benchmarkCacheGetConcurrent(c Cache, b *testing.B) {
	c.Set("foo1", "bar")
	c.Set("foo2", "bar")
	c.Set("foo3", "bar")
	c.Set("foo4", "bar")
	c.Set("foo5", "bar")
	c.Set("foo6", "bar")
	c.Set("foo7", "bar")

	wg := new(sync.WaitGroup)
	workers := runtime.NumCPU()
	each := b.N / workers
	wg.Add(workers)

	b.ResetTimer()
	for i := 0; i < workers; i++ {
		go func() {
			for j := 0; j < each; j++ {
				c.Get("foo1")
				c.Get("foo2")
				c.Get("foo3")
				c.Get("foo5")
				c.Get("foo6")
				c.Get("foo7")
				c.Get("foo8") // doesn't exist
			}
			wg.Done()
		}()
	}
	wg.Wait()
}

func benchmarkCacheSet(c Cache, b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Set("foo", "bar")
	}
}

func benchmarkCacheSetConcurrent(c Cache, b *testing.B) {
	wg := new(sync.WaitGroup)
	workers := runtime.NumCPU()
	each := b.N / workers
	wg.Add(workers)

	b.ResetTimer()
	for i := 0; i < workers; i++ {
		go func() {
			for j := 0; j < each; j++ {
				c.Set("foo1", "bar")
				c.Set("foo2", "bar")
				c.Set("foo3", "bar")
				c.Set("foo4", "bar")
				c.Set("foo5", "bar")
				c.Set("foo6", "bar")
				c.Set("foo7", "bar")
			}
			wg.Done()
		}()
	}
	wg.Wait()
}

func benchmarkCacheGetSetConcurrent(c Cache, b *testing.B) {
	c.Set("foo1", "bar")
	c.Set("foo2", "bar")
	c.Set("foo3", "bar")
	c.Set("foo4", "bar")
	c.Set("foo5", "bar")
	c.Set("foo6", "bar")
	c.Set("foo7", "bar")

	wg := new(sync.WaitGroup)
	workers := runtime.NumCPU()
	each := b.N / workers
	wg.Add(workers)

	b.ResetTimer()
	for i := 0; i < workers; i++ {
		go func() {
			for j := 0; j < each; j++ {
				c.Get("foo1")
				c.Get("foo2")
				c.Get("foo3")
				c.Get("foo5")
				c.Get("foo6")
				c.Get("foo7")
				c.Get("foo8") // doesn't exist

				c.Set("foo1", "bar")
			}
			wg.Done()
		}()
	}
	wg.Wait()
}

func benchmarkCacheSetRemove(c Cache, b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		name := "foo" + strconv.Itoa(i)
		c.Set(name, "bar")
		c.Remove(name)
	}
}
