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

package registry

import (
	"istio.io/istio/pkg/test/framework/component"
	"istio.io/istio/pkg/test/framework/dependency"
)

// Registry of test components.
type Registry struct {
	componentMap map[dependency.Instance]component.Component
}

// New creates a new registry instance.
func New() *Registry {
	return &Registry{
		componentMap: make(map[dependency.Instance]component.Component),
	}
}

// Register a component.
func (r *Registry) Register(c component.Component) {
	r.componentMap[c.ID()] = c
}

// Get returns the component with the given ID.
func (r *Registry) Get(id dependency.Instance) (component.Component, bool) {
	c, ok := r.componentMap[id]
	return c, ok
}
