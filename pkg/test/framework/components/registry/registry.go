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
	"fmt"

	"istio.io/istio/pkg/test/framework/component"
	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
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

// Init initializes the component with the given name, and returns its instance.
func (r *Registry) Init(id dependency.Instance, depMap map[dependency.Instance]interface{}, ctx environment.ComponentContext) (interface{}, error) {
	c, found := r.Get(id)
	if !found {
		return nil, fmt.Errorf("component not found: name:%q, environment:%q", id.String(), ctx.Environment().EnvironmentID())
	}

	return c.Init(ctx, depMap)
}
