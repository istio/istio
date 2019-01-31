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
	"testing"

	"istio.io/istio/pkg/test/framework/api/lifecycle"
)

// Factory for new component instances
type Factory interface {

	// Creates a new component with the given descriptor and scope. The name parameter may be used
	// to create multiple instances of a single descriptor, if multiple are not needed use "".
	NewComponent(name string, d Descriptor, scope lifecycle.Scope) (Instance, error)

	// Creates a new component with the given descriptor and scope, failing if the component cannot
	// be created. The name parameter may be used to create multiple instances of a single descriptor.
	NewComponentOrFail(name string, d Descriptor, scope lifecycle.Scope, t testing.TB) Instance
}
