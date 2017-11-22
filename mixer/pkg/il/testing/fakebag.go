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
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/il"
)

func NewFakeBag(attrs map[string]interface{}) attribute.Bag {
	for k, v := range attrs {
		if sm, ok := v.(map[string]string); ok {
			attrs[k] = NewStringMap(k, sm)
		}
	}
	return &fakeBag{Attrs: attrs}
}

// fakeBag is a fake implementation of the Bag for testing purposes.
type fakeBag struct {
	Attrs map[string]interface{}
}

var _ attribute.Bag = (*fakeBag)(nil)

// Get returns an attribute value.
func (b *fakeBag) Get(name string) (interface{}, bool) {
	c, found := b.Attrs[name]
	return c, found
}

// Names return the names of all the attributes known to this bag.
func (b *fakeBag) Names() []string {
	return []string{}
}

// Done indicates the bag can be reclaimed.
func (b *fakeBag) Done() {}

// DebugString is needed to implement the Bag interface.
func (b *fakeBag) DebugString() string { return "" }

func NewStringMap(name string, entries map[string]string) il.StringMap {
	return stringMap{Name: name, Entries: entries}
}

type stringMap struct {
	// Name of the stringmap  -- request.headers
	Name string
	// Entries in the stringmap
	Entries map[string]string
}

// Get returns a stringmap value and records access
func (s stringMap) Get(key string) (string, bool) {
	str, found := s.Entries[key]
	return str, found
}
