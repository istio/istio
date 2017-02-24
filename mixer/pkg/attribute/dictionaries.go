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

// The code in here is pretty simple. It has a global lock that's held whenever doing anything.
// So this isn't great for high contention scenarios. The code also iterates through the dictionaries
// while holding the lock.
//
// This is all fine and dandy, since this code ends up being seldom used. Typically, this
// is called only when a gRPC stream is initiated, which is a pretty rare situation.
// And typically, there will be 1 dictionary for all of the mixer.

import (
	"sync"
)

type dictionary map[int32]string

type dictionaryEntry struct {
	// immutable dictionary
	dictionary dictionary

	// current use count
	useCount int
}

// dictionaries provides a thread safe interning solution for
// attribute dictionaries. This allows identical
// dictionaries to be shared within the mixer, which reduces
// memory consumption and improves processor cache efficiency.
type dictionaries struct {
	sync.Mutex
	entries []dictionaryEntry
}

// See if two dictionaries are identical
func compareDictionaries(d1 dictionary, d2 dictionary) bool {
	if len(d1) != len(d2) {
		return false
	}

	for k, v := range d1 {
		if d2[k] != v {
			return false
		}
	}

	return true
}

// Intern inserts a dictionary into the interning tables.
// If another equivalent dictionary is already in the tables, then that
// dictionary instance is returned. Otherwise, the input dictionary is
// returned.
//
// This function maintains a use count. When the caller is
// done with a dictionary, it should call the Release function to
// decrement the use count. When the use count of a dictionary drops to
// 0, it is removed from the interning tables such that it can be cleaned up
// by the GC.
//
// Callers must treat the returned map as immutable, otherwise bad bad things
// will happen.
//
// This call is thread-safe.
func (d *dictionaries) Intern(dict dictionary) dictionary {
	d.Lock()
	defer d.Unlock()

	for i := range d.entries {
		entry := &d.entries[i]
		if compareDictionaries(dict, entry.dictionary) {
			entry.useCount++
			return entry.dictionary
		}
	}

	d.entries = append(d.entries, dictionaryEntry{dict, 1})
	return dict
}

// Release decrements the use count of the given dictionary. If
// the use count drops to 0, the dictionary is removed from the
// interning tables such that it can be cleaned up by the GC.
//
// This call is thread-safe.
func (d *dictionaries) Release(dict dictionary) {
	d.Lock()
	defer d.Unlock()

	for i := range d.entries {
		entry := &d.entries[i]
		if compareDictionaries(dict, entry.dictionary) {

			// if use count drops to zero, remove from the list
			entry.useCount--
			if entry.useCount == 0 {
				d.entries = append(d.entries[:i], d.entries[i+1:]...)
			}
			break
		}
	}
}
