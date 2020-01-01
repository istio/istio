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

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/go-cmp/cmp"
)

// Schemas contains metadata about configuration resources.
type Schemas struct {
	byCollection map[string]Schema
}

// SchemasBuilder is a builder for the schemas type.
type SchemasBuilder struct {
	schemas Schemas
}

// NewSchemasBuilder returns a new instance of SchemasBuilder.
func NewSchemasBuilder() *SchemasBuilder {
	s := Schemas{
		byCollection: make(map[string]Schema),
	}

	return &SchemasBuilder{
		schemas: s,
	}
}

// Add a new collection to the schemas.
func (b *SchemasBuilder) Add(s Schema) error {
	if _, found := b.schemas.byCollection[s.Name().String()]; found {
		return fmt.Errorf("collection already exists: %v", s.Name())
	}

	b.schemas.byCollection[s.Name().String()] = s
	return nil
}

// MustAdd calls Add and panics if it fails.
func (b *SchemasBuilder) MustAdd(s Schema) *SchemasBuilder {
	if err := b.Add(s); err != nil {
		panic(fmt.Sprintf("SchemasBuilder.MustAdd: %v", err))
	}
	return b
}

// UnregisterSchemas unregisters all schemas in s from this builder.
func (b *SchemasBuilder) UnregisterSchemas(s Schemas) *SchemasBuilder {
	for _, info := range s.All() {
		b.Remove(info.Name())
	}
	return b
}

// Remove a Schema from the builder.
func (b *SchemasBuilder) Remove(c Name) { // nolint:interfacer
	delete(b.schemas.byCollection, c.String())
}

// Build a new schemas from this SchemasBuilder.
func (b *SchemasBuilder) Build() Schemas {
	s := b.schemas

	// Avoid modify after Build.
	b.schemas = Schemas{}

	return s
}

// Find looks up a Schema by its collection name.
func (s Schemas) Find(collection string) (Schema, bool) {
	i, ok := s.byCollection[collection]
	return i, ok
}

// MustFind calls Find and panics if not found.
func (s Schemas) MustFind(collection string) Schema {
	i, ok := s.Find(collection)
	if !ok {
		panic(fmt.Sprintf("schemas.MustFind: matching entry not found for collection: %q", collection))
	}
	return i
}

// FindByGroupAndKind searches and returns the resource spec with the given group/kind
func (s Schemas) FindByGroupAndKind(group, kind string) (Schema, bool) {
	for _, rs := range s.byCollection {
		if rs.Group() == group && rs.Kind() == kind {
			return rs, true
		}
	}

	return nil, false
}

// MustFind calls FindByGroupAndKind and panics if not found.
func (s Schemas) MustFindByGroupAndKind(group, kind string) Schema {
	r, found := s.FindByGroupAndKind(group, kind)
	if !found {
		panic(fmt.Sprintf("Schemas.MustFindByGroupAndKind: unable to find %s/%s", group, kind))
	}
	return r
}

// All returns all known Schemas
func (s Schemas) All() []Schema {
	result := make([]Schema, 0, len(s.byCollection))

	for _, info := range s.byCollection {
		result = append(result, info)
	}

	return result
}

// CollectionNames returns all known collections.
func (s Schemas) CollectionNames() Names {
	result := make(Names, 0, len(s.byCollection))

	for _, info := range s.byCollection {
		result = append(result, info.Name())
	}

	sort.Slice(result, func(i, j int) bool {
		return strings.Compare(result[i].String(), result[j].String()) < 0
	})

	return result
}

// DisabledCollectionNames returns the names of disabled collections
func (s Schemas) DisabledCollectionNames() Names {
	disabledCollections := make(Names, 0)
	for _, i := range s.byCollection {
		if i.IsDisabled() {
			disabledCollections = append(disabledCollections, i.Name())
		}
	}
	return disabledCollections
}

// Validate the schemas. Returns error if there is a problem.
func (s Schemas) Validate() error {
	for _, c := range s.All() {
		if err := c.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (s Schemas) Equal(o Schemas) bool {
	return cmp.Equal(s.byCollection, o.byCollection)
}
