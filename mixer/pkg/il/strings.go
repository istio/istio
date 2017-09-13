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

package il

import (
	"sync"
)

const (
	// maxStrings is the maximum number of strings that can be placed in the strings table.
	maxStrings = 100
	nullString = "<<DEADBEEF>>"
)

// StringTable is a table that maps uint32 ids to strings and allows efficient storage of strings in IL.
type StringTable struct {
	stringToID map[string]uint32
	idToString []string
	nextID     uint32
	lock       sync.RWMutex
}

// newStringTable creates a new StringTable.
func newStringTable() *StringTable {

	t := &StringTable{
		stringToID: make(map[string]uint32),
		idToString: make([]string, maxStrings),
		nextID:     0,
	}

	t.GetID(nullString)

	return t
}

// GetID returns the id of the given string. The string is added to the table if it doesn't exist.
func (t *StringTable) GetID(s string) uint32 {

	// Assume we can resolve this within reader lock most of the time.
	t.lock.RLock()
	id, exists := t.stringToID[s]
	t.lock.RUnlock()
	if exists {
		return id
	}

	// If not found, go into read-write lock.
	t.lock.Lock()
	defer t.lock.Unlock()

	id, exists = t.stringToID[s]
	if !exists {
		t.stringToID[s] = t.nextID
		t.idToString[t.nextID] = s
		id = t.nextID
		t.nextID++
	}

	return id
}

// TryGetID returns the id of the given string if it exists, or returns 0.
func (t *StringTable) TryGetID(s string) uint32 {
	t.lock.RLock()
	id, exists := t.stringToID[s]
	t.lock.RUnlock()
	if !exists {
		return 0
	}
	return id
}

// GetString returns the string with the given id, or empty string.
func (t *StringTable) GetString(id uint32) string {
	t.lock.RLock()
	defer t.lock.RUnlock()
	return t.idToString[id]
}
