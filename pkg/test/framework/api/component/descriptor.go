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

package component

import (
	"fmt"
)

var _ Requirement = &Descriptor{}

// Variant supports multiple components with the same ID. For example, a test may use two Widget
// components: "fake widget" and "real widget".
type Variant string

// CreateDescriptor creates a descriptor with the given ID and variant. Variants of a component type
// will override the properties of the default Descriptor for that component type if set. For example
// setting a new Configuration or Requires value will override the default Configuration or Requires
// values. This can be used to create multiple instances of a component type with different
// configuration and requirements.
func NewDescriptor(id ID, variant Variant) *Descriptor {
	return &Descriptor{Key: Key{ID: id, Variant: variant}}
}

// Descriptor describes a component of the testing framework.
type Descriptor struct {
	Key               Key
	IsSystemComponent bool
	Configuration     Configuration
	Requires          []Requirement
}

// Key returns the key for this Descriptor, which includes the ID and Variant.
func (d Descriptor) GetKey() Key {
	return d.Key
}

// FriendlyName provides a brief one-liner containing ID and Variant. Useful for error messages.
func (d Descriptor) FriendlyName() string {
	return d.Key.String()
}

func (d Descriptor) String() string {
	result := "Descriptor{\n"

	result += fmt.Sprintf("Key:                %v\n", d.Key)
	result += fmt.Sprintf("IsSystemComponent:  %t\n", d.IsSystemComponent)
	result += fmt.Sprintf("Configuration:      %s\n", d.Configuration)
	result += fmt.Sprintf("Requires:           %v\n", d.Requires)
	result += "}"

	return result
}
