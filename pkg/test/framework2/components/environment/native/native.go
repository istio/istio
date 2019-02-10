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

package native

import (
	"istio.io/istio/pkg/test/framework2/components/environment"
)

const (
	// Name of the environment
	Name = "native"
)

// Environment for testing natively on the host machine. It implements api.Environment, and also
// hosts publicly accessible methods that are specific to local environment.
type Environment struct {
	//// Mesh for configuring pilot.
	//Mesh *meshConfig.MeshConfig

	//// ServiceManager for all deployed services.
	//ServiceManager *service.Manager
}

var _ environment.Instance = &Environment{}

// New returns a new native environment.
func New(_ environment.Context) (environment.Instance, error) {
	return &Environment{}, nil
}

// Type implements environment.Instance
func (e *Environment) Name() string {
	return Name
}
