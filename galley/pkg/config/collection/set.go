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

package collection

import (
	"sort"
	"strings"

	"istio.io/istio/pkg/config/schema/collection"
)

// Set of collections
type Set struct {
	collections map[collection.Name]*Instance
}

// NewSet returns a new set of collections for the given schemas.
func NewSet(schemas collection.Schemas) *Set {
	c := make(map[collection.Name]*Instance)
	schemas.ForEach(func(s collection.Schema) (done bool) {
		c[s.Name()] = New(s)
		return
	})

	return &Set{
		collections: c,
	}
}

// NewSetFromCollections creates a new set based on the given collections
func NewSetFromCollections(collections []*Instance) *Set {
	c := make(map[collection.Name]*Instance, len(collections))
	for _, col := range collections {
		c[col.schema.Name()] = col
	}

	return &Set{
		collections: c,
	}
}

// Collection returns the named collection
func (s *Set) Collection(n collection.Name) *Instance {
	return s.collections[n]
}

// Clone the set.
func (s *Set) Clone() *Set {
	c := make(map[collection.Name]*Instance, len(s.collections))
	for k, v := range s.collections {
		c[k] = v.Clone()
	}

	return &Set{
		collections: c,
	}
}

// Names of the collections in the set.
func (s *Set) Names() collection.Names {
	result := make([]collection.Name, 0, len(s.collections))
	for name := range s.collections {
		result = append(result, name)
	}

	sort.Slice(result, func(i, j int) bool {
		return strings.Compare(result[i].String(), result[j].String()) < 0
	})

	return result
}
