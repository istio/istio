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
	"time"
)

// Bag is a generic mechanism to access a set of attributes.
//
// Bags are thread-safe.
type Bag interface {
	// String returns the named attribute if it exists.
	String(name string) (string, bool)

	// StringKeys returns a snapshot of all keys corresponding to string attributes this bag knows about.
	StringKeys() []string

	// Int64 returns the named attribute if it exists.
	Int64(name string) (int64, bool)

	// Int64Keys returns a snapshot of all keys corresponding to int64 attributes this bag knows about.
	Int64Keys() []string

	// Float64 returns the named attribute if it exists.
	Float64(name string) (float64, bool)

	// Float64Keys returns a snapshot of all keys corresponding to float64 attributes this bag knows about.
	Float64Keys() []string

	// Bool returns the named attribute if it exists.
	Bool(name string) (bool, bool)

	// BoolKeys returns a snapshot of all keys corresponding to bool attributes this bag knows about.
	BoolKeys() []string

	// Time returns the named attribute if it exists.
	Time(name string) (time.Time, bool)

	// TimeKeys returns a snapshot of all keys corresponding to time attributes this bag knows about.
	TimeKeys() []string

	// Bytes returns the named attribute if it exists.
	Bytes(name string) ([]uint8, bool)

	// ByteKeys returns a snapshot of all keys corresponding to byte attributes this bag knows about.
	BytesKeys() []string

	// Done indicates the bag can be reclaimed.
	Done()
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

// Value returns an attribute value from a bag, without having
// to know a priori the type of the attribute in question.
func Value(b Bag, name string) (interface{}, bool) {
	if r, found := b.String(name); found {
		return r, true
	}

	if r, found := b.Int64(name); found {
		return r, true
	}

	if r, found := b.Float64(name); found {
		return r, true
	}

	if r, found := b.Bool(name); found {
		return r, true
	}

	if r, found := b.Time(name); found {
		return r, true
	}

	if r, found := b.Bytes(name); found {
		return r, true
	}

	return nil, false
}
