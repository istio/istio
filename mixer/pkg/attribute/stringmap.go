// Copyright 2018 Istio Authors
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
	"fmt"
	"reflect"

	mixerpb "istio.io/api/mixer/v1"
)

// StringMap wraps a map[string]string and optionally reference counts it
type StringMap struct {
	// name of the stringmap  -- request.headers
	name string
	// entries in the stringmap
	entries map[string]string
	// protoBag that owns this stringmap
	pb *ProtoBag
}

// NewStringMap instantiates a new string map.
func NewStringMap(name string) StringMap {
	return StringMap{
		name:    name,
		entries: make(map[string]string, 1),
	}
}

// WrapStringMap wraps a string map value without reference tracking.
func WrapStringMap(entries map[string]string) StringMap {
	return StringMap{entries: entries}
}

// Set wraps a string map set operation.
func (s StringMap) Set(key, val string) {
	s.entries[key] = val
}

// Get returns a stringmap value and records access
func (s StringMap) Get(key string) (string, bool) {
	str, found := s.entries[key]

	// the string map may be detached from the owning bag
	if s.pb != nil {
		cond := mixerpb.ABSENCE
		if found {
			cond = mixerpb.EXACT
		}

		// TODO add REGEX condition
		s.pb.trackMapReference(s.name, key, cond)
	}

	return str, found
}

// Entries returns the wrapped string map.
func (s StringMap) Entries() map[string]string {
	return s.entries
}

func (s StringMap) copyValue() StringMap {
	c := make(map[string]string, len(s.entries))
	for k2, v2 := range s.entries {
		c[k2] = v2
	}
	return StringMap{name: s.name, entries: c, pb: s.pb}
}

// String returns a string representation of the entries in the string map
func (s StringMap) String() string {
	return fmt.Sprintf("string%v", s.entries)
}

// Equal compares the string map entries
func (s StringMap) Equal(t StringMap) bool {
	return reflect.DeepEqual(s.entries, t.entries)
}
