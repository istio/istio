// Copyright 2017 Istio Authors.
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

package test

import (
	"time"

	"istio.io/mixer/pkg/attribute"
)

// Bag is a test version of attribute.Bag
type Bag struct {
	attribute.Bag

	Strs  map[string]string
	Times map[string]time.Time
}

// NewBag creates a new bag for testing.
func NewBag() *Bag {
	return &Bag{
		Strs:  make(map[string]string),
		Times: make(map[string]time.Time),
	}
}

// String returns the named attribute if it exists.
func (t *Bag) String(name string) (string, bool) {
	v, found := t.Strs[name]
	return v, found
}

// Time returns the named attribute if it exists.
func (t *Bag) Time(name string) (time.Time, bool) {
	v, found := t.Times[name]
	return v, found
}

// Int64 returns the named attribute if it exists.
func (t *Bag) Int64(name string) (int64, bool) {
	return 0, false
}

// Float64 returns the named attribute if it exists.
func (t *Bag) Float64(name string) (float64, bool) {
	return 0, false
}

// Bool returns the named attribute if it exists.
func (t *Bag) Bool(name string) (bool, bool) {
	return false, false
}

// Duration returns the named attribute if it exists.
func (t *Bag) Duration(name string) (time.Duration, bool) {
	return 0, false
}

// Bytes returns the named attribute if it exists.
func (t *Bag) Bytes(name string) ([]uint8, bool) {
	return []uint8{}, false
}

// StringMap returns the named attribute if it exists.
func (t *Bag) StringMap(name string) (map[string]string, bool) {
	return nil, false
}

// DebugString returns the empty string.
// TODO: use attribute.GetMutableBagForTest
func (t *Bag) DebugString() string { return "" }
