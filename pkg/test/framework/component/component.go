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
	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
)

// Component defines the interface for a component of the test framework.
type Component interface {
	// ID returns the unique identifier for this component.
	ID() dependency.Instance
	// Requires returns all components that are required by this component.
	Requires() []dependency.Instance
	// Init creates a new resource that is initialized for the given environment.
	Init(ctx environment.ComponentContext, deps map[dependency.Instance]interface{}) (interface{}, error)
}
