// Copyright 2016 Istio Authors
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
	"bytes"
	"fmt"
	"sort"
	"sync"
)

// MutableBag is a generic mechanism to read and write a set of attributes.
//
// Bags can be chained together in a parent/child relationship. A child bag
// represents a delta over a parent. By default a child looks identical to
// the parent. But as mutations occur to the child, the two start to diverge.
// Resetting a child makes it look identical to its parent again.
type MutableBag struct {
	parent Bag
	values map[string]interface{}
}

var mutableBags = sync.Pool{
	New: func() interface{} {
		return &MutableBag{
			values: make(map[string]interface{}),
		}
	},
}

// GetMutableBag returns an initialized bag.
//
// Bags can be chained in a parent/child relationship. You can pass nil if the
// bag has no parent.
//
// When you are done using the mutable bag, call the Done method to recycle it.
func GetMutableBag(parent Bag) *MutableBag {
	mb := mutableBags.Get().(*MutableBag)

	if parent == nil {
		mb.parent = empty
	} else {
		mb.parent = parent
	}

	return mb
}

// GetMutableBagForTesting returns a Mutable bag based on the specified map
// Use this function only for testing purposes.
func GetMutableBagForTesting(values map[string]interface{}) *MutableBag {
	m := GetMutableBag(nil)
	m.values = values
	for k, v := range values {
		if !CheckType(v) {
			panic(fmt.Errorf("unexpected type for the testing bag %T: %q = %q", v, k, v))
		}
	}
	return m
}

// CopyBag makes a deep copy of a bag.
func CopyBag(b Bag) *MutableBag {
	mb := GetMutableBag(nil)
	for _, k := range b.Names() {
		v, _ := b.Get(k)
		mb.Set(k, copyValue(v))
	}

	return mb
}

// Given an attribute value, create a deep copy of it
func copyValue(v interface{}) interface{} {
	switch t := v.(type) {
	case []byte:
		c := make([]byte, len(t))
		copy(c, t)
		return c

	case StringMap:
		return t.copyValue()
	}

	return v
}

// Done indicates the bag can be reclaimed.
func (mb *MutableBag) Done() {
	// prevent use of a bag that's in the pool
	if mb.parent == nil {
		panic(fmt.Errorf("attempt to use a bag after its Done method has been called"))
	}

	mb.parent = nil
	mb.Reset()
	mutableBags.Put(mb)
}

// Get returns an attribute value.
func (mb *MutableBag) Get(name string) (interface{}, bool) {
	// prevent use of a bag that's in the pool
	if mb.parent == nil {
		panic(fmt.Errorf("attempt to use a bag after its Done method has been called"))
	}

	var r interface{}
	var b bool
	if r, b = mb.values[name]; !b {
		r, b = mb.parent.Get(name)
	}
	return r, b
}

// Contains returns true if the key is present in the bag.
func (mb *MutableBag) Contains(key string) bool {
	if _, found := mb.values[key]; found {
		return true
	}

	return mb.parent.Contains(key)
}

// Names returns the names of all the attributes known to this bag.
func (mb *MutableBag) Names() []string {
	if mb == nil {
		return []string{}
	}

	if mb.parent == nil {
		panic(fmt.Errorf("attempt to use a bag after its Done method has been called"))
	}

	parentNames := mb.parent.Names()

	m := make(map[string]bool, len(parentNames)+len(mb.values))
	for _, name := range parentNames {
		m[name] = true
	}

	for name := range mb.values {
		m[name] = true
	}

	i := 0
	names := make([]string, len(m))
	for name := range m {
		names[i] = name
		i++
	}

	return names
}

// Set creates an override for a named attribute.
func (mb *MutableBag) Set(name string, value interface{}) {
	// prevent use of a bag that's in the pool
	if mb.parent == nil {
		panic(fmt.Errorf("attempt to use a bag after its Done method has been called"))
	}

	if !CheckType(value) {
		panic(fmt.Errorf("invalid type %T for %q with value %v", value, name, value))
	}

	mb.values[name] = value
}

// Delete removes a named item from the local state.
// The item may still be present higher in the hierarchy
func (mb *MutableBag) Delete(name string) {
	delete(mb.values, name)
}

// Reset removes all local state.
func (mb *MutableBag) Reset() {
	mb.values = make(map[string]interface{})
}

// Merge combines an array of bags into the current bag. If the current bag already defines
// a particular attribute, it keeps its value and is not overwritten.
//
// Note that this does a 'shallow' merge. Only the value defined explicitly in the
// mutable bags themselves, and not in any of their parents, are considered.
func (mb *MutableBag) Merge(bag *MutableBag) {
	for k, v := range bag.values {
		// the input bags cannot override values already in the destination bag
		if !mb.Contains(k) {
			mb.values[k] = copyValue(v)
		}
	}
}

// String prints out the attributes from the parent bag, then
// walks through the local changes and prints them as well.
func (mb *MutableBag) String() string {
	if len(mb.values) == 0 {
		return mb.parent.String()
	}

	buf := &bytes.Buffer{}
	buf.WriteString(mb.parent.String())
	buf.WriteString("---\n")

	keys := make([]string, 0, len(mb.values))
	for key := range mb.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		fmt.Fprintf(buf, "%-30s: %v\n", key, mb.values[key])
	}
	return buf.String()
}

// ReferenceTracker keeps track of bag accesses (optionally)
func (mb *MutableBag) ReferenceTracker() ReferenceTracker {
	return nil
}
