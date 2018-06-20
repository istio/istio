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
	byKind map[Kind]Info
	byURL  map[string]Info
}

// NewSchema returns a new instance of Schema.
func NewSchema() *Schema {
	return &Schema{
		byKind: make(map[Kind]Info),
		byURL:  make(map[string]Info),
	}
}

// Register a proto into the schema.
func (s *Schema) Register(protoMessageName string) {
	info := Info{
		Kind:    Kind(protoMessageName),
		TypeURL: fmt.Sprintf("%s/%s", BaseTypeURL, protoMessageName),
	}

	s.byKind[info.Kind] = info
	s.byURL[info.TypeURL] = info
}

// LookupByKind looks up a resource.Info by its kind.
func (s *Schema) LookupByKind(kind Kind) (Info, bool) {
	i, ok := s.byKind[kind]
	return i, ok
}

// LookupByTypeURL looks up a resource.Info by its type url.
func (s *Schema) LookupByTypeURL(url string) (Info, bool) {
	i, ok := s.byURL[url]
	return i, ok
}

// All returns all known info objects
func (s *Schema) All() []Info {
	result := make([]Info, 0, len(s.byKind))

	for _, info := range s.byKind {
		result = append(result, info)
	}

	return result
}

// TypeURLs returns all known type URLs.
func (s *Schema) TypeURLs() []string {
	result := make([]string, 0, len(s.byKind))

	for _, info := range s.byKind {
		result = append(result, info.TypeURL)
	}

	return result
}
