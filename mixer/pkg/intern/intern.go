// Copyright 2016 Google Inc.
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

// Package intern exposes a general purpose string interning model to reduce memory
// consumption and improve processor cache efficiency.
package intern

import (
	"sync"
)

// Pool is container of interned strings.
type Pool struct {
	sync.RWMutex
	strings     map[string]string
	currentSize int
	maxSize     int
}

const (
	// just a guess of course...
	averageStringLength = 10
)

// NewPool allocates a new interning pool ready for use.
//
// Go doesn't currently have sophisticated GC primitives, such as weak pointers.
// As a result, a simple string interning solution can easily become subject to
// memory bloating. Strings are only ever added and never removed, leading to
// an eventual OOM condition. Can easily be leveraged to DDoS a server for example.
//
// The code here uses a simple approach to work around this problem. If the table
// holding the interned strings ever holds more than than maxSize's worth of strings,
// the table is completely dropped on the floor and a new table is allocated. This
// allows any stale strings pointed to by the old table to be reclaimed by the GC.
// This effectively puts a cap on the memory held by any single pool. The cost of
// this approach of course is that interning will be less efficient.
func NewPool(maxSize int) *Pool {
	return &Pool{strings: make(map[string]string, maxSize/averageStringLength), maxSize: maxSize}
}

// Intern returns a sharable version of the string, allowing the
// parameter's storage to be garbage collected.
func (p *Pool) Intern(s string) string {
	// quick try if its already in the table
	p.RLock()
	result, found := p.strings[s]
	p.RUnlock()

	if !found {
		// look again under a serializing r/w lock
		p.Lock()
		if result, found = p.strings[s]; !found {
			if len(s) > p.maxSize-p.currentSize {
				p.strings = make(map[string]string, p.maxSize/averageStringLength)
				p.currentSize = 0
			}

			p.strings[s] = s
			p.currentSize += len(s)
			result = s
		}
		p.Unlock()
	}

	return result
}
