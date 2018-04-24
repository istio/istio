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

package local

import (
	"testing"

	"istio.io/istio/pkg/test"
)

// Environment a local environment for testing.
type Environment struct {
	T testing.TB
}

// StartAPIServer starts the k8s API server
func (e *Environment) StartAPIServer() test.DeployedAPIServer {
	return nil
}

// StartGalley starts the Galley server
func (e *Environment) StartGalley() test.DeployedIstioComponent {
	return nil
}

// StartMixer starts the Mixer server
func (e *Environment) StartMixer() test.DeployedIstioComponent {
	return nil
}

// GetAPIServer returns the deployed k8s API server
func (e *Environment) GetAPIServer() test.DeployedAPIServer {
	return nil
}

// GetIstioComponent gets the deployed configuration for all Istio components of the given kind.
func (e *Environment) GetIstioComponent(k test.DeployedServiceKind) []test.DeployedIstioComponent {
	return nil
}
