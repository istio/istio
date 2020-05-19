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

package il

const (
	// allocSize is the allocation size when extending the string table.
	allocSize  = 512
	nullString = "<<DEADBEEF>>"
)

// StringTable is a table that maps uint32 ids to strings and allows efficient storage of strings in IL.
type StringTable struct {
	stringToID map[string]uint32
	idToString []string
	nextID     uint32
}

// newStringTable creates a new StringTable.
func newStringTable() *StringTable {

	t := &StringTable{
		stringToID: make(map[string]uint32),
		idToString: make([]string, allocSize),
		nextID:     0,
	}

	t.Add(nullString)

	return t
}

// Add adds the given string to the table, if it doesn't exit, and returns its id.
func (t *StringTable) Add(s string) uint32 {
	id, exists := t.stringToID[s]
	if !exists {
		id = t.nextID
		t.nextID++

		if len(t.idToString) <= int(id) {
			tmp := make([]string, len(t.idToString)+allocSize)
			copy(tmp, t.idToString)
			t.idToString = tmp
		}

		t.stringToID[s] = id
		t.idToString[id] = s
	}

	return id
}

// TryGetID returns the id of the given string if it exists, or returns 0.
func (t *StringTable) TryGetID(s string) uint32 {
	id, exists := t.stringToID[s]
	if !exists {
		return 0
	}
	return id
}

// GetString returns the string with the given id, or empty string.
func (t *StringTable) GetString(id uint32) string {
	return t.idToString[id]
}

// Size returns the number of entries in the table.
func (t *StringTable) Size() int {
	return int(t.nextID)
}
