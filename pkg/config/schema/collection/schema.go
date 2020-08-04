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

	"istio.io/istio/pkg/config/schema/resource"
)

// Schema for a collection.
type Schema interface {
	fmt.Stringer

	// Name of the collection.
	Name() Name

	// VariableName is a utility method used to help with codegen. It provides the name of a Schema instance variable.
	VariableName() string

	// Resource is the schema for resources contained in this collection.
	Resource() resource.Schema

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
	Name         string
	VariableName string
	Disabled     bool
	Resource     resource.Schema
}

// Build a Schema instance.
func (b Builder) Build() (Schema, error) {
	if !IsValidName(b.Name) {
		return nil, fmt.Errorf("invalid collection name: %s", b.Name)
	}
	if b.Resource == nil {
		return nil, fmt.Errorf("collection %s: resource must be non-nil", b.Name)
	}

	return &schemaImpl{
		name:         NewName(b.Name),
		variableName: b.VariableName,
		disabled:     b.Disabled,
		resource:     b.Resource,
	}, nil
}

// MustBuild calls Build and panics if it fails.
func (b Builder) MustBuild() Schema {
	s, err := b.Build()
	if err != nil {
		panic(fmt.Sprintf("MustBuild: %v", err))
	}

	return s
}

type schemaImpl struct {
	resource     resource.Schema
	name         Name
	variableName string
	disabled     bool
}

// String interface method implementation.
func (s *schemaImpl) String() string {
	return fmt.Sprintf("[Schema](%s, %q, %s)", s.name, s.resource.ProtoPackage(), s.resource.Proto())
}

func (s *schemaImpl) Name() Name {
	return s.name
}

func (s *schemaImpl) VariableName() string {
	return s.variableName
}

func (s *schemaImpl) Resource() resource.Schema {
	return s.resource
}

func (s *schemaImpl) IsDisabled() bool {
	return s.disabled
}

func (s *schemaImpl) Disable() Schema {
	if s.disabled {
		return s
	}

	cpy := *s
	cpy.disabled = true
	return &cpy
}

func (s *schemaImpl) Equal(o Schema) bool {
	return s.name == o.Name() &&
		s.disabled == o.IsDisabled() &&
		s.Resource().Equal(o.Resource())
}
