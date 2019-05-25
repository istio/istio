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
)

// List wraps a list of values and optionally reference counts it
type List struct {
	// name of the list
	name string
	// entries in the list
	entries []interface{}
}

// NewList creates a new list
func NewList(name string) *List {
	return &List{
		name:    name,
		entries: make([]interface{}, 0, 1),
	}
}

// NewListForTesting should only be used for testing.
func NewListForTesting(name string, entries []interface{}) *List {
	return &List{
		name:    name,
		entries: entries,
	}
}

// Append mutates a list by appending an element to the end.
func (l *List) Append(val interface{}) {
	l.entries = append(l.entries, val)
}

// String returns the string representation of the entries in the list
func (l *List) String() string {
	return fmt.Sprintf("%v", l.entries)
}

// Equal compares the list entries
func (l *List) Equal(m *List) bool {
	return reflect.DeepEqual(l.entries, m.entries)
}
