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

package ilt

import (
	"fmt"
	"sort"
	"sync"

	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/il"
)

// NewFakeBag creates a FakeBag and converts map[string]string to StringMap
func NewFakeBag(attrs map[string]interface{}) *FakeBag {
	bag := &FakeBag{
		Attrs: make(map[string]interface{}, len(attrs)),
	}
	for k, v := range attrs {
		if sm, ok := v.(map[string]string); ok {
			bag.Attrs[k] = NewStringMap(k, sm, bag)
		} else {
			bag.Attrs[k] = v
		}
	}

	bag.referenced = make(map[string]bool)
	return bag
}

// FakeBag is a fake implementation of the Bag for testing purposes.
type FakeBag struct {
	Attrs map[string]interface{}

	referenced     map[string]bool
	referencedLock sync.RWMutex
}

var _ attribute.Bag = (*FakeBag)(nil)

// Get returns an attribute value.
func (b *FakeBag) Get(name string) (interface{}, bool) {
	c, found := b.Attrs[name]
	b.referencedLock.Lock()
	b.referenced[name] = true
	b.referencedLock.Unlock()
	return c, found
}

// Names return the names of all the attributes known to this bag.
func (b *FakeBag) Names() []string {
	return []string{}
}

// Done indicates the bag can be reclaimed.
func (b *FakeBag) Done() {}

// Contains returns true if the key is present in the bag.
func (b *FakeBag) Contains(key string) bool {
	_, found := b.Attrs[key]
	return found
}

// String is needed to implement the Bag interface.
func (b *FakeBag) String() string { return "" }

// ReferencedList returns the sorted list of attributes that were referenced. Attribute references through
// string maps are encoded as mapname[keyname].
func (b *FakeBag) ReferencedList() []string {

	attributes := make([]string, 0, len(b.referenced))

	b.referencedLock.RLock()
	for k := range b.referenced {
		attributes = append(attributes, k)
	}
	b.referencedLock.RUnlock()

	sort.Strings(attributes)

	return attributes
}

// NewStringMap creates an il.StringMap given map[string]string
func NewStringMap(name string, entries map[string]string, parent *FakeBag) il.StringMap {
	return stringMap{Name: name, Entries: entries, parent: parent}
}

type stringMap struct {
	// Name of the stringmap  -- request.headers
	Name string
	// Entries in the stringmap
	Entries map[string]string

	parent *FakeBag
}

// Get returns a stringmap value and records access
func (s stringMap) Get(key string) (string, bool) {
	str, found := s.Entries[key]
	if s.parent != nil {
		name := fmt.Sprintf(`%s[%s]`, s.Name, key)
		s.parent.referencedLock.Lock()
		s.parent.referenced[name] = true
		s.parent.referencedLock.Unlock()
	}
	return str, found
}
