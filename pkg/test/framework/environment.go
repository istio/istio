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

package framework

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework/components/apps/api"
	"istio.io/istio/pkg/test/framework/dependency"
	env "istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/internal"
)

type environment struct {
	ctx        *internal.TestContext
	controller internal.EnvironmentController
}

var _ env.Environment = &environment{}

func (e *environment) CreateTmpDirectory(t testing.TB, name string) string {
	t.Helper()

	//	return createTmpDirectory(t.workDir, t.runID, name)
	s, err := internal.CreateTmpDirectory(e.ctx.Settings().WorkDir, e.ctx.Settings().RunID, name)
	if err != nil {
		e.controller.DumpState(t.Name())
		t.Fatalf("failed creating temp folder: %v", err)
	}

	return s
}

// Configure applies the given configuration to the mesh.
func (e *environment) Configure(t testing.TB, config string) {
	t.Helper()

	if err := e.controller.Configure(config); err != nil {
		e.controller.DumpState(t.Name())
		t.Fatalf("Error applying configuration to dependencies: %v", err)
	}
}

// Evaluate the template against standard set of parameters
func (e *environment) Evaluate(t testing.TB, template string) string {
	t.Helper()

	str, err := e.controller.Evaluate(template)
	if err != nil {
		e.controller.DumpState(t.Name())
		t.Fatalf("Error evaluating template: %v", err)
	}
	return str
}

// GetImplementation returns an interface tha can be used to access the internal implementation details
// of the underlying environment.
func (e *environment) GetImplementation() env.Implementation {
	return e.controller
}

// GetMixer returns a deployed Mixer instance in the internalEnv.
func (e *environment) GetMixer() (env.DeployedMixer, error) {
	s, err := e.get(dependency.Mixer)
	if err != nil {
		return nil, err
	}
	return s.(env.DeployedMixer), nil
}

// GetMixerOrFail returns a deployed Mixer instance in the environment, or fails the test if unsuccessful.
func (e *environment) GetMixerOrFail(t testing.TB) env.DeployedMixer {
	t.Helper()

	m, err := e.GetMixer()
	if err != nil {
		e.controller.DumpState(t.Name())
		t.Fatal(err)
	}

	return m
}

// GetPilot returns a deployed Pilot instance in the internalEnv.
func (e *environment) GetPilot() (env.DeployedPilot, error) {
	p, err := e.get(dependency.Pilot)
	if err != nil {
		return nil, err
	}
	return p.(env.DeployedPilot), nil
}

// GetPilotOrFail returns a deployed Pilot instance in the environment, or fails the test if unsuccessful.
func (e *environment) GetPilotOrFail(t testing.TB) env.DeployedPilot {
	t.Helper()

	m, err := e.GetPilot()
	if err != nil {
		e.controller.DumpState(t.Name())
		t.Fatal(err)
	}

	return m
}

// GetCitadel returns a deployed Citadel instance in the environment.
func (e *environment) GetCitadel() (env.DeployedCitadel, error) {
	p, err := e.get(dependency.Citadel)
	if err != nil {
		return nil, err
	}
	return p.(env.DeployedCitadel), nil
}

// GetCitadelOrFail returns a deployed Citadel instance in the environment, or fails the test if unsuccessful.
func (e *environment) GetCitadelOrFail(t testing.TB) env.DeployedCitadel {
	t.Helper()

	m, err := e.GetCitadel()
	if err != nil {
		t.Fatal(err)
	}

	return m
}

// GetApp returns a fake testing app object for the given name.
func (e *environment) GetApp(name string) (env.DeployedApp, error) {
	s, err := e.get(dependency.Apps)
	if err != nil {
		return nil, err
	}
	return s.(api.Apps).GetApp(name)
}

// GetAppOrFail returns a fake testing app object for the given name, or fails the test if unsuccessful.
func (e *environment) GetAppOrFail(name string, t testing.TB) env.DeployedApp {
	t.Helper()

	m, err := e.GetApp(name)
	if err != nil {
		e.controller.DumpState(t.Name())
		t.Fatal(err)
	}

	return m
}

// GetFortioApp returns a Fortio App object for the given name.
func (e *environment) GetFortioApp(name string) (env.DeployedFortioApp, error) {
	s, err := e.get(dependency.FortioApps)
	if err != nil {
		return nil, err
	}
	return s.(env.DeployedFortioApp), nil
}

// GetFortioAppOrFail returns a Fortio App object for the given name, or fails the test if unsuccessful.
func (e *environment) GetFortioAppOrFail(name string, t testing.TB) env.DeployedFortioApp {
	t.Helper()
	a, err := e.GetFortioApp(name)
	if err != nil {
		e.controller.DumpState(t.Name())
		t.Fatal(err)
	}
	return a
}

// GetFortioApps returns a set of Fortio Apps based on the given selector.
func (e *environment) GetFortioApps(selector string, t testing.TB) []env.DeployedFortioApp {
	t.Helper()
	// TODO: Implement or remove the method
	// See https://github.com/istio/istio/issues/6171
	panic("Not yet implemented")
}

// GetPolicyBackendOrFail returns the mock policy backend that is used by Mixer for policy checks and reports.
func (e *environment) GetPolicyBackendOrFail(t testing.TB) env.DeployedPolicyBackend {
	t.Helper()
	return e.getOrFail(t, dependency.PolicyBackend).(env.DeployedPolicyBackend)
}

func (e *environment) get(dep dependency.Instance) (interface{}, error) {
	s, ok := e.ctx.Tracker.Get(dep)
	if !ok {
		return nil, fmt.Errorf("dependency not initialized: %v", dep)
	}

	return s, nil
}

// GetAPIServer returns a handle to the ambient API Server in the internalEnv.
func (e *environment) GetAPIServer() (env.DeployedAPIServer, error) {
	a, err := e.get(dependency.APIServer)
	if err != nil {
		return nil, err
	}

	return a.(env.DeployedAPIServer), nil
}

// GetAPIServerOrFail returns a handle to the ambient API Server in the environment, or fails the test if
// unsuccessful.
func (e *environment) GetAPIServerOrFail(t testing.TB) env.DeployedAPIServer {
	t.Helper()

	m, err := e.GetAPIServer()
	if err != nil {
		e.controller.DumpState(t.Name())
		t.Fatal(err)
	}

	return m
}

func (e *environment) getOrFail(t testing.TB, dep dependency.Instance) interface{} {
	s, ok := e.ctx.Tracker.Get(dep)
	if !ok {
		e.controller.DumpState(t.Name())
		t.Fatalf("Dependency not initialized: %v", dep)
	}
	return s
}

func (e *environment) ComponentContext() env.ComponentContext {
	return e.ctx
}
