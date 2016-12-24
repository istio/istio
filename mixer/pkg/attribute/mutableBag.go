// Copyright 2016 Google Inc.
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

package attribute

import (
	"sync"
	"time"
)

type mutableBag struct {
	sync.RWMutex
	parent   Bag
	strings  map[string]string
	int64s   map[string]int64
	float64s map[string]float64
	bools    map[string]bool
	times    map[string]time.Time
	bytes    map[string][]uint8
}

var mutableBags = sync.Pool{
	New: func() interface{} {
		return &mutableBag{
			strings:  make(map[string]string),
			int64s:   make(map[string]int64),
			float64s: make(map[string]float64),
			bools:    make(map[string]bool),
			times:    make(map[string]time.Time),
			bytes:    make(map[string][]uint8),
		}
	},
}

func getMutableBag(parent Bag) *mutableBag {
	mb := mutableBags.Get().(*mutableBag)
	mb.parent = parent
	return mb
}

func (mb *mutableBag) Done() {
	mb.Reset()
	mb.parent = nil
	mutableBags.Put(mb)
}

func (mb *mutableBag) String(name string) (string, bool) {
	var r string
	var b bool
	mb.RLock()
	if r, b = mb.strings[name]; !b {
		r, b = mb.parent.String(name)
	}
	mb.RUnlock()
	return r, b
}

func (mb *mutableBag) SetString(name string, value string) {
	mb.Lock()
	mb.strings[name] = value
	mb.Unlock()
}

func (mb *mutableBag) Int64(name string) (int64, bool) {
	var r int64
	var b bool
	mb.RLock()
	if r, b = mb.int64s[name]; !b {
		r, b = mb.parent.Int64(name)
	}
	mb.RUnlock()
	return r, b
}

func (mb *mutableBag) SetInt64(name string, value int64) {
	mb.Lock()
	mb.int64s[name] = value
	mb.Unlock()
}

func (mb *mutableBag) Float64(name string) (float64, bool) {
	var r float64
	var b bool
	mb.RLock()
	if r, b = mb.float64s[name]; !b {
		r, b = mb.parent.Float64(name)
	}
	mb.RUnlock()
	return r, b
}

func (mb *mutableBag) SetFloat64(name string, value float64) {
	mb.Lock()
	mb.float64s[name] = value
	mb.Unlock()
}

func (mb *mutableBag) Bool(name string) (bool, bool) {
	var r bool
	var b bool
	mb.RLock()
	if r, b = mb.bools[name]; !b {
		r, b = mb.parent.Bool(name)
	}
	mb.RUnlock()
	return r, b
}

func (mb *mutableBag) SetBool(name string, value bool) {
	mb.Lock()
	mb.bools[name] = value
	mb.Unlock()
}

func (mb *mutableBag) Time(name string) (time.Time, bool) {
	var r time.Time
	var b bool
	mb.RLock()
	if r, b = mb.times[name]; !b {
		r, b = mb.parent.Time(name)
	}
	mb.RUnlock()
	return r, b
}

func (mb *mutableBag) SetTime(name string, value time.Time) {
	mb.Lock()
	mb.times[name] = value
	mb.Unlock()
}

func (mb *mutableBag) Bytes(name string) ([]uint8, bool) {
	var r []uint8
	var b bool
	mb.RLock()
	if r, b = mb.bytes[name]; !b {
		r, b = mb.parent.Bytes(name)
	}
	mb.RUnlock()
	return r, b
}

func (mb *mutableBag) SetBytes(name string, value []uint8) {
	mb.Lock()
	mb.bytes[name] = value
	mb.Unlock()
}

func (mb *mutableBag) Reset() {
	mb.Lock()

	// my kingdom for a clear method on maps!

	for k := range mb.strings {
		delete(mb.strings, k)
	}

	for k := range mb.int64s {
		delete(mb.int64s, k)
	}

	for k := range mb.float64s {
		delete(mb.float64s, k)
	}

	for k := range mb.bools {
		delete(mb.bools, k)
	}

	for k := range mb.times {
		delete(mb.times, k)
	}

	for k := range mb.bytes {
		delete(mb.bytes, k)
	}

	mb.Unlock()
}

func (mb *mutableBag) Child() MutableBag {
	return getMutableBag(mb)
}
