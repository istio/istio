// Copyright 2019 Istio Authors
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

package collection

// Set of collections
type Set struct {
	collections map[Name]*Instance
}

// NewSet returns a new set of collections
func NewSet(names []Name) *Set {
	c := make(map[Name]*Instance)
	for _, n := range names {
		c[n] = New(n)
	}

	return &Set{
		collections: c,
	}
}

// NewSetFromCollections creates a new set based on the given collections
func NewSetFromCollections(collections []*Instance) *Set {
	c := make(map[Name]*Instance, len(collections))
	for _, col := range collections {
		c[col.collection] = col
	}

	return &Set{
		collections: c,
	}
}

// Collection returns the named collection
func (s *Set) Collection(n Name) *Instance {
	return s.collections[n]
}

// Clone the set.
func (s *Set) Clone() *Set {
	c := make(map[Name]*Instance, len(s.collections))
	for k, v := range s.collections {
		c[k] = v.Clone()
	}

	return &Set{
		collections: c,
	}
}
