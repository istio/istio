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

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/go-multierror"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

// Schemas contains metadata about configuration resources.
type Schemas struct {
	byCollection map[config.GroupVersionKind]resource.Schema
	byAddOrder   []resource.Schema
}

// SchemasFor is a shortcut for creating Schemas. It uses MustAdd for each element.
func SchemasFor(schemas ...resource.Schema) Schemas {
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
		byCollection: make(map[config.GroupVersionKind]resource.Schema),
	}

	return &SchemasBuilder{
		schemas: s,
	}
}

// Add a new collection to the schemas.
func (b *SchemasBuilder) Add(s resource.Schema) error {
	if _, found := b.schemas.byCollection[s.GroupVersionKind()]; found {
		return fmt.Errorf("collection already exists: %v", s.GroupVersionKind())
	}

	b.schemas.byCollection[s.GroupVersionKind()] = s
	b.schemas.byAddOrder = append(b.schemas.byAddOrder, s)
	return nil
}

// MustAdd calls Add and panics if it fails.
func (b *SchemasBuilder) MustAdd(s resource.Schema) *SchemasBuilder {
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
func (s Schemas) ForEach(handleSchema func(resource.Schema) (done bool)) {
	for _, schema := range s.byAddOrder {
		if handleSchema(schema) {
			return
		}
	}
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

func (s Schemas) Intersect(otherSchemas Schemas) Schemas {
	resultBuilder := NewSchemasBuilder()

	schemaLookup := sets.String{}
	for _, myschema := range s.All() {
		schemaLookup.Insert(myschema.String())
	}

	// Only add schemas that are in both sets
	for _, myschema := range otherSchemas.All() {
		if schemaLookup.Contains(myschema.String()) {
			_ = resultBuilder.Add(myschema)
		}
	}
	return resultBuilder.Build()
}

// FindByGroupVersionKind searches and returns the first schema with the given GVK
func (s Schemas) FindByGroupVersionKind(gvk config.GroupVersionKind) (resource.Schema, bool) {
	for _, rs := range s.byAddOrder {
		if rs.GroupVersionKind() == gvk {
			return rs, true
		}
	}

	return nil, false
}

// FindByGroupVersionAliasesKind searches and returns the first schema with the given GVK,
// if not found, it will search for version aliases for the schema to see if there is a match.
func (s Schemas) FindByGroupVersionAliasesKind(gvk config.GroupVersionKind) (resource.Schema, bool) {
	for _, rs := range s.byAddOrder {
		for _, va := range rs.GroupVersionAliasKinds() {
			if va == gvk {
				return rs, true
			}
		}
	}
	return nil, false
}

// FindByGroupKind searches and returns the first schema with the given GVK, ignoring versions.
// Generally it's a good idea to use FindByGroupVersionAliasesKind, which validates the version as well.
// FindByGroupKind provides future proofing against versions we don't yet know about; given we don't know them, its risky.
func (s Schemas) FindByGroupKind(gvk config.GroupVersionKind) (resource.Schema, bool) {
	for _, rs := range s.byAddOrder {
		if rs.Group() == gvk.Group && rs.Kind() == gvk.Kind {
			return rs, true
		}
	}
	return nil, false
}

// FindByGroupVersionResource searches and returns the first schema with the given GVR
func (s Schemas) FindByGroupVersionResource(gvr schema.GroupVersionResource) (resource.Schema, bool) {
	for _, rs := range s.byAddOrder {
		if rs.GroupVersionResource() == gvr {
			return rs, true
		}
	}

	return nil, false
}

// All returns all known Schemas
func (s Schemas) All() []resource.Schema {
	return slices.Clone(s.byAddOrder)
}

// GroupVersionKinds returns all known GroupVersionKinds
func (s Schemas) GroupVersionKinds() []config.GroupVersionKind {
	res := []config.GroupVersionKind{}
	for _, r := range s.All() {
		res = append(res, r.GroupVersionKind())
	}
	return res
}

// Add creates a copy of this Schemas with the given schemas added.
func (s Schemas) Add(toAdd ...resource.Schema) Schemas {
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
func (s Schemas) Remove(toRemove ...resource.Schema) Schemas {
	b := NewSchemasBuilder()

	for _, s := range s.byAddOrder {
		shouldAdd := true
		for _, r := range toRemove {
			if r.Equal(s) {
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

// Kinds returns all known resource kinds.
func (s Schemas) Kinds() []string {
	kinds := sets.NewWithLength[string](len(s.byAddOrder))
	for _, s := range s.byAddOrder {
		kinds.Insert(s.Kind())
	}

	out := kinds.UnsortedList()
	return slices.Sort(out)
}

// Validate the schemas. Returns error if there is a problem.
func (s Schemas) Validate() (err error) {
	for _, c := range s.byAddOrder {
		err = multierror.Append(err, c.Validate()).ErrorOrNil()
	}
	return err
}

func (s Schemas) Equal(o Schemas) bool {
	return cmp.Equal(s.byAddOrder, o.byAddOrder)
}
