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

package internal

import "istio.io/istio/pkg/test/environment"

// Stateful is an interface for managing the life-cycle of stateful dependencies.
type Stateful interface {

	// Initialize the dependency. The returned value can be used to store state, and will be passed back
	// for reset and cleanup.
	Initialize(environment.Interface) (interface{}, error)

	// Reset will be called prior to the start of a test for resetting the state of the dependency, if needed.
	Reset(environment.Interface, interface{}) error

	// Cleanup the dependency after a test run.
	Cleanup(environment.Interface, interface{})
}
