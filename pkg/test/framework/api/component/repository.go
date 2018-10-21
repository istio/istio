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

import "testing"

// Repository of components.
type Repository interface {
	// Gets the component with the given ID, or null if not found.
	GetComponent(id ID) Instance
	GetComponentOrFail(id ID, t testing.TB) Instance

	// Gets the component matching the given descriptor, or null if not found.
	GetComponentForDescriptor(d Descriptor) Instance
	GetComponentForDescriptorOrFail(d Descriptor, t testing.TB) Instance

	// Gets all components currently active in the system.
	GetAllComponents() []Instance
}
