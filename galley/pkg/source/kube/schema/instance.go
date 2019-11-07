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

package schema

import (
	"sort"
	"strings"
)

// Instance represents a schema for a set of known Kubernetes resource types.
type Instance struct {
	specs []ResourceSpec
}

// All returns information about all known types.
func (s *Instance) All() []ResourceSpec {
	return s.specs
}

// Get returns the schema for the given kind, or nil if not found.
func (s *Instance) Get(kind string) *ResourceSpec {
	for _, v := range s.All() {
		if v.Kind == kind {
			return &v
		}
	}
	return nil
}

// New is a simplified Instance factory method when all specs can be provided as arguments.
func New(spec ...ResourceSpec) *Instance {
	return NewBuilder().Add(spec...).Build()
}

// Builder is a builder for schema.
type Builder struct {
	schema *Instance
}

// NewBuilder returns a new instance of a Builder.
func NewBuilder() *Builder {
	return &Builder{
		schema: &Instance{},
	}
}

// Add a new ResourceSpec to the schema.
func (b *Builder) Add(spec ...ResourceSpec) *Builder {
	b.schema.specs = append(b.schema.specs, spec...)
	return b
}

// Build a new instance of schema. The specs will be sorted by resource name.
func (b *Builder) Build() *Instance {
	s := b.schema

	// Avoid modify after Build.
	b.schema = nil

	// Sort the specs by their resource names.
	sort.Slice(s.specs, func(i, j int) bool {
		return strings.Compare(s.specs[i].CanonicalResourceName(), s.specs[j].CanonicalResourceName()) < 0
	})

	return s
}
