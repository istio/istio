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

package components

import (
	"fmt"

	"istio.io/istio/pkg/test/framework/components/apiserver"
	"istio.io/istio/pkg/test/framework/components/mixer"
	"istio.io/istio/pkg/test/framework/components/policybackend"
	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/settings"
)

// All known test components.
var All = newRegistry()

func init() {
	// Register all known components here.
	All.register(dependency.APIServer, settings.Kubernetes, apiserver.InitKube)
	All.register(dependency.Mixer, settings.Local, mixer.InitLocal)
	All.register(dependency.Mixer, settings.Kubernetes, mixer.InitKube)
	All.register(dependency.PolicyBackend, settings.Local, policybackend.InitLocal)
	All.register(dependency.PolicyBackend, settings.Kubernetes, policybackend.InitKube)
}

// Registry of test components.
type Registry struct {
	initializers map[key]InitFn
}

type key struct {
	dependency  dependency.Instance
	environment string
}

// New registry is created and returned
func newRegistry() *Registry {
	return &Registry{
		initializers: make(map[key]InitFn),
	}
}

// Register a component.
func (r *Registry) register(d dependency.Instance, e string, initFn InitFn) {
	r.initializers[key{dependency: d, environment: e}] = initFn
}

// Init initializes the component with the given name, and returns its instance.
func (r *Registry) Init(d dependency.Instance, ctx environment.ComponentContext) (interface{}, error) {
	envName := ctx.Environment().EnvironmentName()
	init, found := r.initializers[key{dependency: d, environment: envName}]
	if !found {
		return nil, fmt.Errorf("component not found: name:%q, environment:%q", d.String(), envName)
	}

	return init(ctx)
}

// InitFn is a function for initializing a registered component.
type InitFn func(ctx environment.ComponentContext) (interface{}, error)
