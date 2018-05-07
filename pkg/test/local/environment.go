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

var _ test.Environment = &Environment{}

// Configure applies the given configuration to the mesh.
func (e *Environment) Configure(config string) {
	// TODO
	panic("Not yet implemented")
}

// GetMixer returns a deployed Mixer instance in the environment.
func (e *Environment) GetMixer() test.DeployedMixer {
	// TODO
	panic("Not yet implemented")
}

// GetPilot returns a deployed Pilot instance in the environment.
func (e *Environment) GetPilot() test.DeployedPilot {
	// TODO
	panic("Not yet implemented")
}

// GetApp returns a fake testing app object for the given name.
func (e *Environment) GetApp(name string) (test.DeployedApp, error) {
	// TODO
	panic("Not yet implemented")
}

// GetAppOrFail returns a fake testing app object for the given name, or fails the test if unsuccessful.
func (e *Environment) GetAppOrFail(name string, t *testing.T) test.DeployedApp {
	t.Helper()
	// TODO
	panic("Not yet implemented")
}

// GetFortioApp returns a Fortio App object for the given name.
func (e *Environment) GetFortioApp(name string) (test.DeployedFortioApp, error) {
	// TODO
	panic("Not yet implemented")
}

// GetFortioAppOrFail returns a Fortio App object for the given name, or fails the test if unsuccessful.
func (e *Environment) GetFortioAppOrFail(name string, t *testing.T) (test.DeployedFortioApp, error) {
	t.Helper()
	// TODO
	panic("Not yet implemented")
}

// GetFortioApps returns a set of Fortio Apps based on the given selector.
func (e *Environment) GetFortioApps(selector string, t *testing.T) []test.DeployedFortioApp {
	t.Helper()
	// TODO
	panic("Not yet implemented")
}

// GetPolicyBackendOrFail returns the mock policy backend that is used by Mixer for policy checks and reports.
func (e *Environment) GetPolicyBackendOrFail(t *testing.T) test.DeployedPolicyBackend {
	t.Helper()
	// TODO
	panic("Not yet implemented")
}
