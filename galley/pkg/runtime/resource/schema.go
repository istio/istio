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

// BaseTypeURL contains the common scheme & domain parts of all type urls.
const BaseTypeURL = "types.googleapis.com"

// injectable function for overriding proto.MessageType, for testing purposes.
type messageTypeFn func(name string, isGogo bool) reflect.Type

// Schema contains metadata about configuration resources.
type Schema struct {
	byName map[string]Info
	byURL  map[string]Info

	messageTypeFn messageTypeFn
}

// NewSchema returns a new instance of Schema.
func NewSchema() *Schema {
	return newSchema(getProtoMessageType)
}

// NewSchema returns a new instance of Schema.
func newSchema(messageTypeFn messageTypeFn) *Schema {
	return &Schema{
		byName:        make(map[string]Info),
		byURL:         make(map[string]Info),
		messageTypeFn: messageTypeFn,
	}
}

// Register a proto into the schema.
func (s *Schema) Register(protoMessageName string, isGogo bool) {
	if _, found := s.byName[protoMessageName]; found {
		panic(fmt.Sprintf("Schema.Register: Proto type is registered multiple times: %q", protoMessageName))
	}

	// Before registering, ensure that the proto type is actually reachable.
	goType := s.messageTypeFn(protoMessageName, isGogo)
	if goType == nil {
		panic(fmt.Sprintf("Schema.Register: Proto type not found: %q, isGogo: %v", protoMessageName, isGogo))
	}

	info := Info{
		MessageName: MessageName{protoMessageName},
		IsGogo:      isGogo,
		TypeURL:     fmt.Sprintf("%s/%s", BaseTypeURL, protoMessageName),
		goType:      goType,
	}

	s.byName[info.MessageName.string] = info
	s.byURL[info.TypeURL] = info
}

// Info looks up a resource.Info by its message name. It panics if the expected entry not found.
func (s *Schema) Info(name MessageName) Info {
	i, ok := s.byName[name.string]
	if !ok {
		panic(fmt.Sprintf("Schema.Info: MessageName %q not found", name))
	}
	return i
}

// LookupByMessageName looks up a resource.Info by its message name.
func (s *Schema) LookupByMessageName(name string) (Info, bool) {
	i, ok := s.byName[name]
	return i, ok
}

// LookupByTypeURL looks up a resource.Info by its type url.
func (s *Schema) LookupByTypeURL(url string) (Info, bool) {
	i, ok := s.byURL[url]
	return i, ok
}

// All returns all known info objects
func (s *Schema) All() []Info {
	result := make([]Info, 0, len(s.byName))

	for _, info := range s.byName {
		result = append(result, info)
	}

	return result
}

// TypeURLs returns all known type URLs.
func (s *Schema) TypeURLs() []string {
	result := make([]string, 0, len(s.byName))

	for _, info := range s.byName {
		result = append(result, info.TypeURL)
	}

	return result
}
