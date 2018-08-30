//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package resource

import (
	"fmt"
	"reflect"
)

// injectable function for overriding proto.MessageType, for testing purposes.
type messageTypeFn func(name string) reflect.Type

// Schema contains metadata about configuration resources.
type Schema struct {
	byURL map[string]Info

	messageTypeFn messageTypeFn
}

// SchemaBuilder is a buidler for the Schema type.
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
		byURL:         make(map[string]Info),
		messageTypeFn: messageTypeFn,
	}

	return &SchemaBuilder{
		schema: s,
	}
}

// Register a proto into the schema.
func (b *SchemaBuilder) Register(typeURL string) {
	if _, found := b.schema.byURL[typeURL]; found {
		panic(fmt.Sprintf("Schema.Register: Proto type is registered multiple times: %q", typeURL))
	}

	// Before registering, ensure that the proto type is actually reachable.
	url, err := newTypeURL(typeURL)
	if err != nil {
		panic(err)
	}

	goType := b.schema.messageTypeFn(url.MessageName())
	if goType == nil {
		panic(fmt.Sprintf("Schema.Register: Proto type not found: %q", url.MessageName()))
	}

	info := Info{
		TypeURL: url,
		goType:  goType,
	}

	b.schema.byURL[info.TypeURL.String()] = info
}

// Build a new Schema from this SchemaBuilder.
func (b *SchemaBuilder) Build() *Schema {
	s := b.schema

	// Avoid modify after Build.
	b.schema = nil

	return s
}

// Lookup looks up a resource.Info by its type url.
func (s *Schema) Lookup(url string) (Info, bool) {
	i, ok := s.byURL[url]
	return i, ok
}

// Get looks up a resource.Info by its type Url. Panics if it is not found.
func (s *Schema) Get(url string) Info {
	i, ok := s.Lookup(url)
	if !ok {
		panic(fmt.Sprintf("Schema.Get: matching entry not found for url: %q", url))
	}
	return i
}

// All returns all known info objects
func (s *Schema) All() []Info {
	result := make([]Info, 0, len(s.byURL))

	for _, info := range s.byURL {
		result = append(result, info)
	}

	return result
}

// TypeURLs returns all known type URLs.
func (s *Schema) TypeURLs() []string {
	result := make([]string, 0, len(s.byURL))

	for _, info := range s.byURL {
		result = append(result, info.TypeURL.string)
	}

	return result
}
