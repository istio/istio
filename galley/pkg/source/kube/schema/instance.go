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

// Instance represents a set of known Kubernetes resource types.
type Instance struct {
	entries []ResourceSpec
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
func (b *Builder) Add(entry ResourceSpec) {
	b.schema.entries = append(b.schema.entries, entry)
}

// Build a new instance of schema.
func (b *Builder) Build() *Instance {
	s := b.schema

	// Avoid modify after Build.
	b.schema = nil

	return s
}

// All returns information about all known types.
func (e *Instance) All() []ResourceSpec {
	return e.entries
}
