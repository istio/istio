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

package test

import (
	"istio.io/istio/pkg/test/impl/apiserver"
)

// ApiServer dependency. Depending on Linux v.driver. Mac, it will be satisfied in a different way.
var ApiServer = &ApiServerDependency{}

// Represents a typed ApiServer dependency. Once initialized, the caller can access the API Server via
// GetConfig() method.
type ApiServerDependency struct {
}

var _ Dependency = &ApiServerDependency{}

func (a *ApiServerDependency) String() string {
	return "apiserver"
}

func (a *ApiServerDependency) initialize() (interface{}, error) {
	// Use the underlying platform-specific implementation to instantiate a new APIServer instance.
	return apiserver.New()
}

func (a *ApiServerDependency) reset(interface{}) error {
	return nil
}

func (a *ApiServerDependency) cleanup(interface{}) {
}
