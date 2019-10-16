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
	"reflect"
	"sort"
)

// Specs contains metadata about configuration resources.
type Specs struct {
	byCollection map[string]Spec
}

// SpecsBuilder is a builder for the specs type.
type SpecsBuilder struct {
	specs         Specs
	messageTypeFn messageTypeFn
}

// injectable function for overriding proto.MessageType, for testing purposes.
type messageTypeFn func(name string) reflect.Type

// NewSpecsBuilder returns a new instance of SpecsBuilder.
func NewSpecsBuilder() *SpecsBuilder {
	return newSpecsBuilder(getProtoMessageType)
}

// newSpecsBuilder returns a new instance of SpecsBuilder.
func newSpecsBuilder(messageTypeFn messageTypeFn) *SpecsBuilder {
	s := Specs{
		byCollection: make(map[string]Spec),
	}

	return &SpecsBuilder{
		specs:         s,
		messageTypeFn: messageTypeFn,
	}
}

// Add a new collection to the specs.
func (b *SpecsBuilder) Add(s Spec) error {
	if _, found := b.specs.byCollection[s.Name.String()]; found {
		return fmt.Errorf("collection already exists: %v", s.Name)
	}

	b.specs.byCollection[s.Name.String()] = s
	return nil
}

// MustAdd calls Add and panics if it fails.
func (b *SpecsBuilder) MustAdd(s Spec) {
	if err := b.Add(s); err != nil {
		panic(fmt.Sprintf("SpecsBuilder.MustAdd: %v", err))
	}
}

// UnregisterSpecs unregisters all specs in s from this builder.
func (b *SpecsBuilder) UnregisterSpecs(s Specs) *SpecsBuilder {
	for _, info := range s.All() {
		b.Remove(info.Name)
	}
	return b
}

// Remove a Spec from the builder.
func (b *SpecsBuilder) Remove(c Name) { // nolint:interfacer
	delete(b.specs.byCollection, c.String())
}

// Build a new specs from this SpecsBuilder.
func (b *SpecsBuilder) Build() Specs {
	s := b.specs

	// Avoid modify after Build.
	b.specs = Specs{}

	return s
}

// Lookup looks up a Spec by its collection name.
func (s Specs) Lookup(collection string) (Spec, bool) {
	i, ok := s.byCollection[collection]
	return i, ok
}

// Get looks up a resource.Spec by its collection. Panics if it is not found.
func (s Specs) Get(collection string) Spec {
	i, ok := s.Lookup(collection)
	if !ok {
		panic(fmt.Sprintf("specs.Get: matching entry not found for collection: %q", collection))
	}
	return i
}

// All returns all known Specs
func (s Specs) All() []Spec {
	result := make([]Spec, 0, len(s.byCollection))

	for _, info := range s.byCollection {
		result = append(result, info)
	}

	return result
}

// CollectionNames returns all known collections.
func (s Specs) CollectionNames() []string {
	result := make([]string, 0, len(s.byCollection))

	for _, info := range s.byCollection {
		result = append(result, info.Name.String())
	}

	sort.Strings(result)

	return result
}

// Validate the specs. Returns error if there is a problem.
func (s Specs) Validate() error {
	for _, c := range s.All() {
		if err := c.Validate(); err != nil {
			return err
		}
	}
	return nil
}
