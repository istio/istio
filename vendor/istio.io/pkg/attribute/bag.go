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
	"fmt"
	"reflect"
	"time"
)

// Bag is a generic mechanism to access a set of attributes.
//
// The type of an attribute value is guaranteed to be one of the following:
// - int64
// - string
// - float64
// - bool
// - time.Time
// - time.Duration
// - []byte (backed by a byte array)
// - attribute.StringMap (backed by a map[string]string)
//
// Attribute value types are physical representation of the semantic attribute types.
// For example, IP addresses are represented as []byte.
//
// The following types are not fully implemented at the surface level:
// - *attribute.List (note the pointer, backed by []interface{})
type Bag interface {
	fmt.Stringer

	// Get returns an attribute value.
	Get(name string) (value interface{}, found bool)

	// Names returns the names of all the attributes known to this bag.
	Names() []string

	// Contains returns true if this bag contains the specified key.
	Contains(key string) bool

	// Done indicates the bag can be reclaimed.
	Done()

	// ReferenceTracker keeps track of bag accesses (optionally)
	ReferenceTracker() ReferenceTracker
}

// Equal compares two attribute values.
func Equal(this, that interface{}) bool {
	if this == nil && that == nil {
		return true
	}

	switch x := this.(type) {
	case int64, string, float64, bool:
		return x == that
	case time.Time:
		if y, ok := that.(time.Time); ok {
			return x.Equal(y)
		}
	case time.Duration:
		if y, ok := that.(time.Duration); ok {
			return x == y
		}
	case []byte:
		if y, ok := that.([]byte); ok {
			return reflect.DeepEqual(x, y)
		}
	case StringMap:
		if y, ok := that.(StringMap); ok {
			return x.Equal(y)
		}
	case *List:
		if y, ok := that.(*List); ok {
			return x.Equal(y)
		}
	}
	return false
}

// CheckType validates that an attribute value has a supported type.
func CheckType(value interface{}) bool {
	switch value.(type) {
	case int64, string, float64, bool, time.Time, time.Duration, []byte, StringMap, *List:
		return true
	default:
		return false
	}
}
