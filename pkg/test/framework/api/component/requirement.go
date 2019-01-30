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

var (
	_ Requirement = &ConfiguredRequirement{}
)

// Requirement is a marker interface for an element that can be required of the testing framework.
type Requirement fmt.Stringer

// Configuration is a marker interface for configuration objects that components take.
type Configuration fmt.Stringer

// A configured requirement contains the requirement, a configuration object, and the name to give
// that configured object.
type ConfiguredRequirement struct {
	name   string
	req    Requirement
	config Configuration
}

func NewNamedRequirement(name string, requirement Requirement) *ConfiguredRequirement {
	return NewConfiguredRequirement(name, requirement, nil)
}

// Creates a named requirement that includes configuration
func NewConfiguredRequirement(name string, requirement Requirement, config Configuration) *ConfiguredRequirement {
	return &ConfiguredRequirement{name, requirement, config}
}

func (c ConfiguredRequirement) String() string {
	return fmt.Sprint("{name: ", c.name, ", requirement: ", c.req, ", config: ", c.config)
}

func (c *ConfiguredRequirement) GetName() string {
	return c.name
}

func (c *ConfiguredRequirement) GetRequirement() Requirement {
	return c.req
}

func (c *ConfiguredRequirement) GetConfiguration() Configuration {
	return c.config
}
