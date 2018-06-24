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
)

// BaseTypeURL contains the common scheme & domain parts of all type urls.
const BaseTypeURL = "types.googleapis.com"

// Schema contains metadata about configuration resources.
type Schema struct {
	byName map[MessageName]Info
	byURL  map[string]Info
}

// NewSchema returns a new instance of Schema.
func NewSchema() *Schema {
	return &Schema{
		byName: make(map[MessageName]Info),
		byURL:  make(map[string]Info),
	}
}

// Register a proto into the schema.
func (s *Schema) Register(protoMessageName string) {
	info := Info{
		MessageName: MessageName(protoMessageName),
		TypeURL:     fmt.Sprintf("%s/%s", BaseTypeURL, protoMessageName),
	}

	s.byName[info.MessageName] = info
	s.byURL[info.TypeURL] = info
}

// LookupByMessageName looks up a resource.Info by its message name.
func (s *Schema) LookupByMessageName(name MessageName) (Info, bool) {
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
