// Copyright Istio Authors
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

	"istio.io/pkg/attribute"
)

// NewFakeBag creates a FakeBag and converts map[string]string to StringMap
func NewFakeBag(attrs map[string]interface{}) *FakeBag {
	bag := &FakeBag{
		Attrs: make(map[string]interface{}, len(attrs)),
	}
	for k, v := range attrs {
		if sm, ok := v.(map[string]string); ok {
			bag.Attrs[k] = attribute.NewStringMap(k, sm, bag)
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
	b.referenced[name] = found
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

// ReferenceTracker is not set
func (b *FakeBag) ReferenceTracker() attribute.ReferenceTracker {
	return b
}

// ReferencedList returns the sorted list of attributes that were referenced. Attribute references through
// string maps are encoded as mapname[keyname]. Absent values are prefixed with "-".
func (b *FakeBag) ReferencedList() []string {

	attributes := make([]string, 0, len(b.referenced))

	b.referencedLock.RLock()
	for k, found := range b.referenced {
		attr := k
		if !found {
			attr = "-" + attr
		}
		attributes = append(attributes, attr)
	}
	b.referencedLock.RUnlock()

	sort.Strings(attributes)

	return attributes
}

// Reference implements reference tracker interface
func (b *FakeBag) Reference(string, attribute.Presence) {
}

// MapReference implements reference tracker interface
func (b *FakeBag) MapReference(attr, key string, cond attribute.Presence) {
	name := fmt.Sprintf(`%s[%s]`, attr, key)
	b.referencedLock.Lock()
	switch cond {
	case attribute.Exact:
		b.referenced[name] = true
	default:
		b.referenced[name] = false
	}
	b.referencedLock.Unlock()
}

func (b *FakeBag) Clear()                                              {}
func (b *FakeBag) Restore(attribute.ReferencedAttributeSnapshot)       {}
func (b *FakeBag) Snapshot() (_ attribute.ReferencedAttributeSnapshot) { return }
