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

import "fmt"

var _ Requirement = &Descriptor{}

// Variant allows an environment to support multiple components with the same ID. For example, an environment might
// support two Widget components: "fake widget" and "real widget".
type Variant string

// TODO(sven): Remove Variant in favor of support for configuration and names.
// Descriptor describes a component of the testing framework.
type Descriptor struct {
	ID
	IsSystemComponent bool
	Variant           Variant
	Requires          []Requirement
}

// FriendlyName provides a brief one-liner containing ID and Variant. Useful for error messages.
func (d Descriptor) FriendlyName() string {
	if d.Variant != "" {
		return fmt.Sprintf("%s(%s)", d.ID, d.Variant)
	}
	return string(d.ID)
}

func (d *Descriptor) String() string {
	result := ""

	result += fmt.Sprintf("ID:                 %v\n", d.ID)
	result += fmt.Sprintf("IsSystemComponent:  %t\n", d.IsSystemComponent)
	result += fmt.Sprintf("Variant:            %s\n", d.Variant)
	result += fmt.Sprintf("Requires:           %v\n", d.Requires)

	return result
}
