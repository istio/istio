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
type messageTypeFn func(name string, isGogo bool) reflect.Type

// Schema contains metadata about configuration resources.
type Schema struct {
	byURL map[string]Info

	messageTypeFn messageTypeFn
}

// NewSchema returns a new instance of Schema.
func NewSchema() *Schema {
	return newSchema(getProtoMessageType)
}

// NewSchema returns a new instance of Schema.
func newSchema(messageTypeFn messageTypeFn) *Schema {
	return &Schema{
		byURL:         make(map[string]Info),
		messageTypeFn: messageTypeFn,
	}
}

// Register a proto into the schema.
func (s *Schema) Register(typeURL string, isGogo bool) {
	if _, found := s.byURL[typeURL]; found {
		panic(fmt.Sprintf("Schema.Register: Proto type is registered multiple times: %q", typeURL))
	}

	// Before registering, ensure that the proto type is actually reachable.
	url, err := newTypeURL(typeURL)
	if err != nil {
		panic(err)
	}

	goType := s.messageTypeFn(url.messageName(), isGogo)
	if goType == nil {
		panic(fmt.Sprintf("Schema.Register: Proto type not found: %q, isGogo: %v", url.messageName(), isGogo))
	}

	info := Info{
		TypeURL: url,
		IsGogo:  isGogo,
		goType:  goType,
	}

	s.byURL[info.TypeURL.String()] = info
}

// Lookup looks up a resource.Info by its type url.
func (s *Schema) Lookup(url string) (Info, bool) {
	i, ok := s.byURL[url]
	return i, ok
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
