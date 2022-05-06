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
	"fmt"
	"sort"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/go-multierror"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/config"
)

// Schemas contains metadata about configuration resources.
type Schemas struct {
	byCollection map[Name]Schema
	byAddOrder   []Schema
}

// SchemasFor is a shortcut for creating Schemas. It uses MustAdd for each element.
func SchemasFor(schemas ...Schema) Schemas {
	b := NewSchemasBuilder()
	for _, s := range schemas {
		b.MustAdd(s)
	}
	return b.Build()
}

// SchemasBuilder is a builder for the schemas type.
type SchemasBuilder struct {
	schemas Schemas
}

// NewSchemasBuilder returns a new instance of SchemasBuilder.
func NewSchemasBuilder() *SchemasBuilder {
	s := Schemas{
		byCollection: make(map[Name]Schema),
	}

	return &SchemasBuilder{
		schemas: s,
	}
}

// Add a new collection to the schemas.
func (b *SchemasBuilder) Add(s Schema) error {
	if _, found := b.schemas.byCollection[s.Name()]; found {
		return fmt.Errorf("collection already exists: %v", s.Name())
	}

	b.schemas.byCollection[s.Name()] = s
	b.schemas.byAddOrder = append(b.schemas.byAddOrder, s)
	return nil
}

// MustAdd calls Add and panics if it fails.
func (b *SchemasBuilder) MustAdd(s Schema) *SchemasBuilder {
	if err := b.Add(s); err != nil {
		panic(fmt.Sprintf("SchemasBuilder.MustAdd: %v", err))
	}
	return b
}

// Build a new schemas from this SchemasBuilder.
func (b *SchemasBuilder) Build() Schemas {
	s := b.schemas

	// Avoid modify after Build.
	b.schemas = Schemas{}

	return s
}

// ForEach executes the given function on each contained schema, until the function returns true.
func (s Schemas) ForEach(handleSchema func(Schema) (done bool)) {
	for _, schema := range s.byAddOrder {
		if handleSchema(schema) {
			return
		}
	}
}

func (s Schemas) Intersect(otherSchemas Schemas) Schemas {
	resultBuilder := NewSchemasBuilder()
	for _, myschema := range s.All() {
		if _, ok := otherSchemas.FindByGroupVersionResource(myschema.Resource().GroupVersionResource()); ok {
			// an error indicates the schema has already been added, which doesn't negatively impact intersect
			_ = resultBuilder.Add(myschema)
		}
	}
	return resultBuilder.Build()
}

func (s Schemas) Union(otherSchemas Schemas) Schemas {
	resultBuilder := NewSchemasBuilder()
	for _, myschema := range s.All() {
		// an error indicates the schema has already been added, which doesn't negatively impact intersect
		_ = resultBuilder.Add(myschema)
	}
	for _, myschema := range otherSchemas.All() {
		// an error indicates the schema has already been added, which doesn't negatively impact intersect
		_ = resultBuilder.Add(myschema)
	}
	return resultBuilder.Build()
}

// Find looks up a Schema by its collection name.
func (s Schemas) Find(collection string) (Schema, bool) {
	i, ok := s.byCollection[Name(collection)]
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

// FindByGroupVersionKind searches and returns the first schema with the given GVK
func (s Schemas) FindByGroupVersionKind(gvk config.GroupVersionKind) (Schema, bool) {
	for _, rs := range s.byAddOrder {
		if rs.Resource().GroupVersionKind() == gvk {
			return rs, true
		}
	}

	return nil, false
}

// FindByGroupVersionAliasesKind searches and returns the first schema with the given GVK,
// if not found, it will search for version aliases for the schema to see if there is a match.
func (s Schemas) FindByGroupVersionAliasesKind(gvk config.GroupVersionKind) (Schema, bool) {
	for _, rs := range s.byAddOrder {
		if rs.Resource().GroupVersionKind() == gvk {
			return rs, true
		}
		for _, va := range rs.Resource().GroupVersionAliasKinds() {
			if va == gvk {
				return rs, true
			}
		}
	}
	return nil, false
}

// FindByGroupVersionResource searches and returns the first schema with the given GVR
func (s Schemas) FindByGroupVersionResource(gvr schema.GroupVersionResource) (Schema, bool) {
	for _, rs := range s.byAddOrder {
		if rs.Resource().GroupVersionResource() == gvr {
			return rs, true
		}
	}

	return nil, false
}

// FindByPlural searches and returns the first schema with the given Group, Version and plural form of the Kind
func (s Schemas) FindByPlural(group, version, plural string) (Schema, bool) {
	for _, rs := range s.byAddOrder {
		if rs.Resource().Plural() == plural &&
			rs.Resource().Group() == group &&
			rs.Resource().Version() == version {
			return rs, true
		}
	}

	return nil, false
}

// MustFindByGroupVersionKind calls FindByGroupVersionKind and panics if not found.
func (s Schemas) MustFindByGroupVersionKind(gvk config.GroupVersionKind) Schema {
	r, found := s.FindByGroupVersionKind(gvk)
	if !found {
		panic(fmt.Sprintf("Schemas.MustFindByGroupVersionKind: unable to find %s", gvk))
	}
	return r
}

// All returns all known Schemas
func (s Schemas) All() []Schema {
	return append(make([]Schema, 0, len(s.byAddOrder)), s.byAddOrder...)
}

// Add creates a copy of this Schemas with the given schemas added.
func (s Schemas) Add(toAdd ...Schema) Schemas {
	b := NewSchemasBuilder()

	for _, s := range s.byAddOrder {
		b.MustAdd(s)
	}

	for _, s := range toAdd {
		b.MustAdd(s)
	}

	return b.Build()
}

// Remove creates a copy of this Schemas with the given schemas removed.
func (s Schemas) Remove(toRemove ...Schema) Schemas {
	b := NewSchemasBuilder()

	for _, s := range s.byAddOrder {
		shouldAdd := true
		for _, r := range toRemove {
			if r.Name() == s.Name() {
				shouldAdd = false
				break
			}
		}
		if shouldAdd {
			b.MustAdd(s)
		}
	}

	return b.Build()
}

// CollectionNames returns all known collections.
func (s Schemas) CollectionNames() Names {
	result := make(Names, 0, len(s.byAddOrder))

	for _, info := range s.byAddOrder {
		result = append(result, info.Name())
	}

	sort.Slice(result, func(i, j int) bool {
		return strings.Compare(result[i].String(), result[j].String()) < 0
	})

	return result
}

// Kinds returns all known resource kinds.
func (s Schemas) Kinds() []string {
	kinds := make(map[string]struct{}, len(s.byAddOrder))
	for _, s := range s.byAddOrder {
		kinds[s.Resource().Kind()] = struct{}{}
	}

	out := make([]string, 0, len(kinds))
	for kind := range kinds {
		out = append(out, kind)
	}

	sort.Strings(out)
	return out
}

// Validate the schemas. Returns error if there is a problem.
func (s Schemas) Validate() (err error) {
	for _, c := range s.byAddOrder {
		err = multierror.Append(err, c.Resource().Validate()).ErrorOrNil()
	}
	return
}

func (s Schemas) Equal(o Schemas) bool {
	return cmp.Equal(s.byAddOrder, o.byAddOrder)
}
