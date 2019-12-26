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

	"istio.io/istio/galley/pkg/config/meta/schema/resource"
)

// Schema is the metadata for a resource collection.
type Schema struct {
	// Schema for the resources contained within the collection.
	resource.Schema

	// Name of the collection.
	Name     Name
	Disabled bool
}

// NewSchema returns a new instance of Schema, or error if the input is not valid.
func NewSchema(name string, resourceSchema resource.Schema) (Schema, error) {
	if !IsValidName(name) {
		return Schema{}, fmt.Errorf("invalid collection name: %s", name)
	}

	return Schema{
		Name:   NewName(name),
		Schema: resourceSchema,
	}, nil
}

// MustNewSchema calls NewSchema and panics if it fails.
func MustNewSchema(name string, resourceSchema resource.Schema) Schema {
	s, err := NewSchema(name, resourceSchema)
	if err != nil {
		panic(fmt.Sprintf("MustNewSchema: %v", err))
	}

	return s
}

// String interface method implementation.
func (s *Schema) String() string {
	return fmt.Sprintf("[Schema](%s, %q, %s)", s.Name, s.ProtoPackage, s.Proto)
}
