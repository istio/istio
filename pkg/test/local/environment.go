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

	"istio.io/istio/pkg/test/environment"
	"istio.io/istio/pkg/test/internal"
)

// Environment a local environment for testing. It implements environment.Interface, and also
// hosts publicly accessible methods that are specific to local environment.
type Environment struct {
	ctx internal.TestContext
}

var _ environment.Interface = &Environment{}
var _ Internal = &Environment{}

// NewEnvironment returns a new instance of local environment.
func NewEnvironment(ctx internal.TestContext) (*Environment, error) {
	return &Environment{
		ctx: ctx,
	}, nil
}

// Configure applies the given configuration to the mesh.
func (e *Environment) Configure(t testing.TB, config string) {
	// TODO
	panic("Not yet implemented")
}

// GetMixer returns a deployed Mixer instance in the environment.
func (e *Environment) GetMixer() (environment.DeployedMixer, error) {
	// TODO
	panic("Not yet implemented")
}

// GetMixerOrFail returns a deployed Mixer instance in the environment, or fails the test if unsuccessful.
func (e *Environment) GetMixerOrFail(t testing.TB) environment.DeployedMixer {
	t.Helper()

	m, err := e.GetMixer()
	if err != nil {
		t.Fatal(err)
	}

	return m
}

// GetPilot returns a deployed Pilot instance in the environment.
func (e *Environment) GetPilot() (environment.DeployedPilot, error) {
	// TODO
	panic("Not yet implemented")
}

// GetPilotOrFail returns a deployed Pilot instance in the environment, or fails the test if unsuccessful.
func (e *Environment) GetPilotOrFail(t testing.TB) environment.DeployedPilot {
	t.Helper()

	m, err := e.GetPilot()
	if err != nil {
		t.Fatal(err)
	}

	return m
}

// GetApp returns a fake testing app object for the given name.
func (e *Environment) GetApp(name string) (environment.DeployedApp, error) {
	// TODO
	panic("Not yet implemented")
}

// GetAppOrFail returns a fake testing app object for the given name, or fails the test if unsuccessful.
func (e *Environment) GetAppOrFail(name string, t testing.TB) environment.DeployedApp {
	t.Helper()
	// TODO
	panic("Not yet implemented")
}

// GetFortioApp returns a Fortio App object for the given name.
func (e *Environment) GetFortioApp(name string) (environment.DeployedFortioApp, error) {
	// TODO
	panic("Not yet implemented")
}

// GetFortioAppOrFail returns a Fortio App object for the given name, or fails the test if unsuccessful.
func (e *Environment) GetFortioAppOrFail(name string, t testing.TB) environment.DeployedFortioApp {
	t.Helper()
	a, err := e.GetFortioApp(name)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// GetFortioApps returns a set of Fortio Apps based on the given selector.
func (e *Environment) GetFortioApps(selector string, t testing.TB) []environment.DeployedFortioApp {
	t.Helper()
	// TODO
	panic("Not yet implemented")
}

// GetPolicyBackendOrFail returns the mock policy backend that is used by Mixer for policy checks and reports.
func (e *Environment) GetPolicyBackendOrFail(t testing.TB) environment.DeployedPolicyBackend {
	t.Helper()
	// TODO
	panic("Not yet implemented")
}
