/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package common

import (
	"fmt"
)

// ResourceNeeds maps the type to count of resources types needed
type ResourceNeeds map[string]int

// TypeToResources stores all the leased resources with the same type f
type TypeToResources map[string][]Resource

// ConfigType gather the type of config to be applied by Mason in order to construct the resource
type ConfigType struct {
	// Identifier of the struct this maps back to
	Type string `json:"type,omitempty"`
	// Marshaled JSON content
	Content string `json:"content,omitempty"`
}

// MasonConfig holds Mason config information
type MasonConfig struct {
	Configs []ResourcesConfig `json:"configs,flow,omitempty"`
}

// ResourcesConfig holds information to construct a resource.
// The ResourcesConfig Name maps to the Resource Type
// All Resource of a given type will be constructed using the same configuration
type ResourcesConfig struct {
	Name   string        `json:"name"`
	Config ConfigType    `json:"config"`
	Needs  ResourceNeeds `json:"needs"`
}

// ResourcesConfigByName helps sorting ResourcesConfig by name
type ResourcesConfigByName []ResourcesConfig

func (ut ResourcesConfigByName) Len() int           { return len(ut) }
func (ut ResourcesConfigByName) Swap(i, j int)      { ut[i], ut[j] = ut[j], ut[i] }
func (ut ResourcesConfigByName) Less(i, j int) bool { return ut[i].GetName() < ut[j].GetName() }

// GetName implements the item interface for storage
func (conf ResourcesConfig) GetName() string { return conf.Name }

// ItemToResourcesConfig casts an Item object to a ResourcesConfig
func ItemToResourcesConfig(i Item) (ResourcesConfig, error) {
	conf, ok := i.(ResourcesConfig)
	if !ok {
		return ResourcesConfig{}, fmt.Errorf("cannot construct Resource from received object %v", i)
	}
	return conf, nil
}

// Copy returns a copy of the TypeToResources
func (t TypeToResources) Copy() TypeToResources {
	n := TypeToResources{}
	for k, v := range t {
		n[k] = v
	}
	return n
}
