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
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/golang/protobuf/ptypes"

	mixerpb "istio.io/mixer/api/v1"
)

// Bag is a generic mechanism to access a set of attributes.
//
// Bags are thread-safe.
type Bag interface {
	// String returns the named attribute if it exists.
	String(name string) (string, bool)

	// Int64 returns the named attribute if it exists.
	Int64(name string) (int64, bool)

	// Float64 returns the named attribute if it exists.
	Float64(name string) (float64, bool)

	// Bool returns the named attribute if it exists.
	Bool(name string) (bool, bool)

	// Time returns the named attribute if it exists.
	Time(name string) (time.Time, bool)

	// Bytes returns the named attribute if it exists.
	Bytes(name string) ([]uint8, bool)
}

// MutableBag is a generic mechanism to read and write a set of attributes.
//
// Bags can be chained together in a parent/child relationship. A child bag
// represents a delta over a parent. By default a child looks identical to
// the parent. But as mutations occur to the child, the two start to diverge.
// Resetting a child makes it look identical to its parent again.
//
// MutableBags are thread-safe.
type MutableBag interface {
	Bag

	// SetString sets an override for a named attribute.
	SetString(name string, value string)

	// SetInt64 sets an override for a named attribute.
	SetInt64(name string, value int64)

	// SetFloat64 sets an override for a named attribute.
	SetFloat64(name string, value float64)

	// SetBool sets an override for a named attribute.
	SetBool(name string, value bool)

	// SetTime sets an override for a named attribute.
	SetTime(name string, value time.Time)

	// SetBytes sets an override for a named attribute.
	SetBytes(name string, value []uint8)

	// Reset removes all local state
	Reset()

	// Allocates a child mutable bag.
	//
	// Mutating a child doesn't affect the parent's state, all mutations are deltas.
	Child() MutableBag
}

type bag struct {
	sync.RWMutex
	parent   Bag
	strings  map[string]string
	int64s   map[string]int64
	float64s map[string]float64
	bools    map[string]bool
	times    map[string]time.Time
	bytes    map[string][]uint8
}

func (ab *bag) String(name string) (string, bool) {
	var r string
	var b bool
	ab.RLock()
	if r, b = ab.strings[name]; !b {
		if ab.parent != nil {
			r, b = ab.parent.String(name)
		}
	}
	ab.RUnlock()
	return r, b
}

func (ab *bag) SetString(name string, value string) {
	ab.Lock()
	if ab.strings == nil {
		ab.strings = make(map[string]string)
	}
	ab.strings[name] = value
	ab.Unlock()
}

func (ab *bag) Int64(name string) (int64, bool) {
	var r int64
	var b bool
	ab.RLock()
	if r, b = ab.int64s[name]; !b {
		if ab.parent != nil {
			r, b = ab.parent.Int64(name)
		}
	}
	ab.RUnlock()
	return r, b
}

func (ab *bag) SetInt64(name string, value int64) {
	ab.Lock()
	if ab.int64s == nil {
		ab.int64s = make(map[string]int64)
	}
	ab.int64s[name] = value
	ab.Unlock()
}

func (ab *bag) Float64(name string) (float64, bool) {
	var r float64
	var b bool
	ab.RLock()
	if r, b = ab.float64s[name]; !b {
		if ab.parent != nil {
			r, b = ab.parent.Float64(name)
		}
	}
	ab.RUnlock()
	return r, b
}

func (ab *bag) SetFloat64(name string, value float64) {
	ab.Lock()
	if ab.float64s == nil {
		ab.float64s = make(map[string]float64)
	}
	ab.float64s[name] = value
	ab.Unlock()
}

func (ab *bag) Bool(name string) (bool, bool) {
	var r bool
	var b bool
	ab.RLock()
	if r, b = ab.bools[name]; !b {
		if ab.parent != nil {
			r, b = ab.parent.Bool(name)
		}
	}
	ab.RUnlock()
	return r, b
}

func (ab *bag) SetBool(name string, value bool) {
	ab.Lock()
	if ab.bools == nil {
		ab.bools = make(map[string]bool)
	}
	ab.bools[name] = value
	ab.Unlock()
}

func (ab *bag) Time(name string) (time.Time, bool) {
	var r time.Time
	var b bool
	ab.RLock()
	if r, b = ab.times[name]; !b {
		if ab.parent != nil {
			r, b = ab.parent.Time(name)
		}
	}
	ab.RUnlock()
	return r, b
}

func (ab *bag) SetTime(name string, value time.Time) {
	ab.Lock()
	if ab.times == nil {
		ab.times = make(map[string]time.Time)
	}
	ab.times[name] = value
	ab.Unlock()
}

func (ab *bag) Bytes(name string) ([]uint8, bool) {
	var r []uint8
	var b bool
	ab.RLock()
	if r, b = ab.bytes[name]; !b {
		if ab.parent != nil {
			r, b = ab.parent.Bytes(name)
		}
	}
	ab.RUnlock()
	return r, b
}

func (ab *bag) SetBytes(name string, value []uint8) {
	ab.Lock()
	if ab.bytes == nil {
		ab.bytes = make(map[string][]uint8)
	}
	ab.bytes[name] = value
	ab.Unlock()
}

func (ab *bag) Reset() {
	ab.Lock()
	ab.reset()
	ab.Unlock()
}

// Requires the write lock to be held before calling...
func (ab *bag) reset() {
	// Ideally, this would be merely clearing maps instead of reallocating 'em,
	// but there's no way to do that in Go :-(
	ab.strings = nil
	ab.int64s = nil
	ab.float64s = nil
	ab.bools = nil
	ab.times = nil
	ab.bytes = nil
}

func (ab *bag) Child() MutableBag {
	return &bag{parent: ab}
}

// Ensure that all dictionary indices are valid and that all values
// are in range.
//
// Note that since we don't have the attribute schema, this doesn't validate
// that a given attribute is being treated as the right type. That is, an
// attribute called 'source.ip' which is of type IP_ADDRESS could be listed as
// a string or an int, and we wouldn't catch it here.
func checkPreconditions(dictionary dictionary, attrs *mixerpb.Attributes) error {
	for k := range attrs.StringAttributes {
		if _, present := dictionary[k]; !present {
			return fmt.Errorf("attribute index %d is not defined in the current dictionary", k)
		}
	}

	for k := range attrs.Int64Attributes {
		if _, present := dictionary[k]; !present {
			return fmt.Errorf("attribute index %d is not defined in the current dictionary", k)
		}
	}

	for k := range attrs.DoubleAttributes {
		if _, present := dictionary[k]; !present {
			return fmt.Errorf("attribute index %d is not defined in the current dictionary", k)
		}
	}

	for k := range attrs.BoolAttributes {
		if _, present := dictionary[k]; !present {
			return fmt.Errorf("attribute index %d is not defined in the current dictionary", k)
		}
	}

	for k, v := range attrs.TimestampAttributes {
		if _, present := dictionary[k]; !present {
			return fmt.Errorf("attribute index %d is not defined in the current dictionary", k)
		}

		if _, err := ptypes.Timestamp(v); err != nil {
			return err
		}
	}

	for k := range attrs.BytesAttributes {
		if _, present := dictionary[k]; !present {
			return fmt.Errorf("attribute index %d is not defined in the current dictionary", k)
		}
	}

	return nil
}

// Update the state of the bag based on the content of an Attributes struct
func (ab *bag) update(dictionary dictionary, attrs *mixerpb.Attributes) error {
	// check preconditions up front and bail if there are any
	// errors without mutating the context.
	if err := checkPreconditions(dictionary, attrs); err != nil {
		return err
	}

	ab.Lock()

	if attrs.ResetContext {
		ab.reset()
	}

	// apply all attributes
	if attrs.StringAttributes != nil {
		if ab.strings == nil {
			ab.strings = make(map[string]string)
		}

		for k, v := range attrs.StringAttributes {
			ab.strings[dictionary[k]] = v
		}
	}

	if attrs.Int64Attributes != nil {
		if ab.int64s == nil {
			ab.int64s = make(map[string]int64)
		}

		for k, v := range attrs.Int64Attributes {
			ab.int64s[dictionary[k]] = v
		}
	}

	if attrs.DoubleAttributes != nil {
		if ab.float64s == nil {
			ab.float64s = make(map[string]float64)
		}

		for k, v := range attrs.DoubleAttributes {
			ab.float64s[dictionary[k]] = v
		}
	}

	if attrs.BoolAttributes != nil {
		if ab.bools == nil {
			ab.bools = make(map[string]bool)
		}

		for k, v := range attrs.BoolAttributes {
			ab.bools[dictionary[k]] = v
		}
	}

	if attrs.TimestampAttributes != nil {
		if ab.times == nil {
			ab.times = make(map[string]time.Time)
		}

		for k, v := range attrs.TimestampAttributes {
			t, _ := ptypes.Timestamp(v)
			ab.times[dictionary[k]] = t
		}
	}

	if attrs.BytesAttributes != nil {
		if ab.bytes == nil {
			ab.bytes = make(map[string][]uint8)
		}

		for k, v := range attrs.BytesAttributes {
			ab.bytes[dictionary[k]] = v
		}
	}

	// delete requested attributes
	for _, d := range attrs.DeletedAttributes {
		if name, present := dictionary[d]; present {
			delete(ab.strings, name)
			delete(ab.int64s, name)
			delete(ab.float64s, name)
			delete(ab.bools, name)
			delete(ab.times, name)
			delete(ab.bytes, name)
		}
	}

	ab.Unlock()
	return nil
}

// The key type is unexported to prevent collisions with context keys defined in
// other packages.
type key int

const bagKey key = 0

// NewContext returns a new Context carrying the supplied bag.
func NewContext(ctx context.Context, bag MutableBag) context.Context {
	return context.WithValue(ctx, bagKey, bag)
}

// FromContext extracts the bag from ctx, if present.
func FromContext(ctx context.Context) (MutableBag, bool) {
	bag, ok := ctx.Value(bagKey).(MutableBag)
	return bag, ok
}
