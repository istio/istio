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

	"istio.io/istio/galley/pkg/config/schema/resource"
)

// Schema for a collection.
type Schema interface {
	resource.Schema

	// Name of the collection.
	Name() Name

	// IsDisabled indicates whether or not this collection is disabled.
	IsDisabled() bool

	// Disable creates a disabled copy of this Schema.
	Disable() Schema

	// Equal is a helper function for testing equality between Schema instances. This supports comparison
	// with the cmp library.
	Equal(other Schema) bool
}

// Config for the creation of a Schema
type Builder struct {
	// Schema for the resources contained within the collection.
	resource.Schema

	// Name of the collection.
	Name     string
	Disabled bool
}

// Build a Schema instance.
func (b Builder) Build() (Schema, error) {
	if !IsValidName(b.Name) {
		return nil, fmt.Errorf("invalid collection name: %s", b.Name)
	}

	return &immutableSchema{
		name:     NewName(b.Name),
		disabled: b.Disabled,
		Schema:   b.Schema,
	}, nil
}

// MustBuild calls Build and panics if it fails.
func (b Builder) MustBuild() Schema {
	s, err := b.Build()
	if err != nil {
		panic(fmt.Sprintf("MustNewSchema: %v", err))
	}

	return s
}

type immutableSchema struct {
	resource.Schema
	name     Name
	disabled bool
}

// String interface method implementation.
func (s *immutableSchema) String() string {
	return fmt.Sprintf("[Schema](%s, %q, %s)", s.name, s.ProtoPackage(), s.Proto())
}

func (s *immutableSchema) Name() Name {
	return s.name
}

func (s *immutableSchema) IsDisabled() bool {
	return s.disabled
}

func (s *immutableSchema) Disable() Schema {
	if s.disabled {
		return s
	}

	cpy := *s
	cpy.disabled = true
	return &cpy
}

func (s *immutableSchema) Equal(o Schema) bool {
	return s.name == o.Name() &&
		s.disabled == o.IsDisabled() &&
		s.IsClusterScoped() == o.IsClusterScoped() &&
		s.Kind() == o.Kind() &&
		s.Plural() == o.Plural() &&
		s.Group() == o.Group() &&
		s.Version() == o.Version() &&
		s.Proto() == o.Proto() &&
		s.ProtoPackage() == o.ProtoPackage()
}
