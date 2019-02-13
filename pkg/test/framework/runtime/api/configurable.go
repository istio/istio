//  Copyright 2019 Istio Authors
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

package api

import "istio.io/istio/pkg/test/framework/api/component"

// Configurable interface is implemented by Components that accept Configuration.
type Configurable interface {
	// Configure is called by the test framework to configure a component. It should not be called directly by test code.
	// To configure a component, pass the configuration when requiring the component.
	Configure(config component.Configuration) error
}
