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

package resource

import (
	"fmt"
	"reflect"
)

// injectable function for overriding proto.MessageType, for testing purposes.
type messageTypeFn func(name string) reflect.Type

// Schema contains metadata about configuration resources.
type Schema struct {
	byCollection map[string]Info

	messageTypeFn messageTypeFn
}

// SchemaBuilder is a buidler for the schema type.
type SchemaBuilder struct {
	schema *Schema
}

// NewSchemaBuilder returns a new instance of SchemaBuilder.
func NewSchemaBuilder() *SchemaBuilder {
	return newSchemaBuilder(getProtoMessageType)
}

// newSchemaBuilder returns a new instance of SchemaBuilder.
func newSchemaBuilder(messageTypeFn messageTypeFn) *SchemaBuilder {
	s := &Schema{
		byCollection:  make(map[string]Info),
		messageTypeFn: messageTypeFn,
	}

	return &SchemaBuilder{
		schema: s,
	}
}

// Register a proto into the schema.
func (b *SchemaBuilder) Register(rawCollection, rawTypeURL string) Info {
	if _, found := b.schema.byCollection[rawCollection]; found {
		panic(fmt.Sprintf("schema.Register: collection is registered multiple times: %q", rawCollection))
	}

	typeURL, err := newTypeURL(rawTypeURL)
	if err != nil {
		panic(err)
	}

	goType := b.schema.messageTypeFn(typeURL.MessageName())
	if goType == nil {
		panic(fmt.Sprintf("schema.Register: Proto type not found: %q", typeURL.MessageName()))
	}

	info := Info{
		Collection: newCollection(rawCollection),
		goType:     goType,
		TypeURL:    typeURL,
	}

	b.RegisterInfo(info)

	return info
}

// Register a proto into the schema.
func (b *SchemaBuilder) RegisterInfo(info Info) *SchemaBuilder {
	b.schema.byCollection[info.Collection.String()] = info
	return b
}

// Register all protos in the given Schema
func (b *SchemaBuilder) RegisterSchema(schema *Schema) *SchemaBuilder {
	for _, info := range schema.All() {
		b.RegisterInfo(info)
	}
	return b
}

// Unregister all protos in the given Schema
func (b *SchemaBuilder) UnregisterSchema(schema *Schema) *SchemaBuilder {
	for _, info := range schema.All() {
		b.UnregisterInfo(info)
	}
	return b
}

// Unregister a proto from the schema.
func (b *SchemaBuilder) UnregisterInfo(info Info) *SchemaBuilder {
	delete(b.schema.byCollection, info.Collection.String())
	return b
}

// Build a new schema from this SchemaBuilder.
func (b *SchemaBuilder) Build() *Schema {
	s := b.schema

	// Avoid modify after Build.
	b.schema = nil

	return s
}

// Lookup looks up a resource.Info by its collection.
func (s *Schema) Lookup(collection string) (Info, bool) {
	i, ok := s.byCollection[collection]
	return i, ok
}

// Get looks up a resource.Info by its collection. Panics if it is not found.
func (s *Schema) Get(collection string) Info {
	i, ok := s.Lookup(collection)
	if !ok {
		panic(fmt.Sprintf("schema.Get: matching entry not found for collection: %q", collection))
	}
	return i
}

// All returns all known info objects
func (s *Schema) All() []Info {
	result := make([]Info, 0, len(s.byCollection))

	for _, info := range s.byCollection {
		result = append(result, info)
	}

	return result
}

// Collections returns all known collections.
func (s *Schema) Collections() []string {
	result := make([]string, 0, len(s.byCollection))

	for _, info := range s.byCollection {
		result = append(result, info.Collection.string)
	}

	return result
}
