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
	"fmt"
	"testing"

	"istio.io/istio/pilot/pkg/model"
	envoyv1 "istio.io/istio/pilot/pkg/proxy/envoy/v1"
	"istio.io/istio/pkg/test/dependency"
	"istio.io/istio/pkg/test/environment"
	"istio.io/istio/pkg/test/fakes/policy"
	"istio.io/istio/pkg/test/internal"
	"istio.io/istio/pkg/test/local/pilot"
	"istio.io/istio/pkg/test/tmpl"
)

const (
	namespace = "istio-system"
)

// Environment a local environment for testing. It implements environment.Interface, and also
// hosts publicly accessible methods that are specific to local environment.
type Environment struct {
	ctx *internal.TestContext
}

var _ environment.Interface = &Environment{}
var _ internal.Environment = &Environment{}

// NewEnvironment returns a new instance of local environment.
func NewEnvironment() (*Environment, error) {
	return &Environment{}, nil
}

// Initialize the environment. This is called once during the lifetime of the suite.
func (e *Environment) Initialize(ctx *internal.TestContext) error {
	e.ctx = ctx
	return nil
}

// InitializeDependency is called when a new dependency is encountered during test run.
func (e *Environment) InitializeDependency(ctx *internal.TestContext, d dependency.Instance) (interface{}, error) {
	switch d {
	case dependency.Kube:
		return nil, fmt.Errorf("local environment does not support running with a cluster")

	case dependency.Mixer:
		return newMixer(ctx)

	case dependency.PolicyBackend:
		return newPolicyBackend(policy.DefaultPort)

	case dependency.Pilot:
		return newPilot()

	default:
		return nil, fmt.Errorf("unrecognized dependency: %v", d)
	}
}

// Configure applies the given configuration to the mesh.
func (e *Environment) Configure(t testing.TB, config string) {
	for _, d := range e.ctx.Tracker().All() {
		if configurable, ok := d.(internal.Configurable); ok {
			err := configurable.ApplyConfig(config)
			if err != nil {
				t.Fatalf("Error applying configuration to dependencies: %v", err)
			}
		}
	}
	// TODO: Implement a mechanism for reliably waiting for the configuration to disseminate in the system.
	// We can use CtrlZ to expose the config state of Mixer and Pilot.
	// See https://github.com/istio/istio/issues/6169 and https://github.com/istio/istio/issues/6170.
}

// Evaluate the template against standard set of parameters
func (e *Environment) Evaluate(tb testing.TB, template string) string {
	p := tmpl.Parameters{
		TestNamespace:       "test",
		DependencyNamespace: "dependencies",
	}

	s, err := tmpl.Evaluate(template, p)
	if err != nil {
		tb.Fatalf("Error evaluating template: %v", err)
	}

	return s
}

// Reset implementation.
func (e *Environment) Reset() error {
	return nil
}

// GetMixer returns a deployed Mixer instance in the environment.
func (e *Environment) GetMixer() (environment.DeployedMixer, error) {
	s, err := e.get(dependency.Mixer)
	if err != nil {
		return nil, err
	}
	return s.(environment.DeployedMixer), nil
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
	p, err := e.get(dependency.Pilot)
	if err != nil {
		return nil, err
	}
	return p.(environment.DeployedPilot), nil
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
	s, err := e.get(dependency.Apps)
	if err != nil {
		return nil, err
	}
	return s.(environment.DeployedApp), nil
}

// GetAppOrFail returns a fake testing app object for the given name, or fails the test if unsuccessful.
func (e *Environment) GetAppOrFail(name string, t testing.TB) environment.DeployedApp {
	t.Helper()

	m, err := e.GetApp(name)
	if err != nil {
		t.Fatal(err)
	}

	return m
}

// GetFortioApp returns a Fortio App object for the given name.
func (e *Environment) GetFortioApp(name string) (environment.DeployedFortioApp, error) {
	s, err := e.get(dependency.FortioApps)
	if err != nil {
		return nil, err
	}
	return s.(environment.DeployedFortioApp), nil
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
	// TODO: Implement or remove the method
	// See https://github.com/istio/istio/issues/6171
	panic("Not yet implemented")
}

// GetPolicyBackendOrFail returns the mock policy backend that is used by Mixer for policy checks and reports.
func (e *Environment) GetPolicyBackendOrFail(t testing.TB) environment.DeployedPolicyBackend {
	t.Helper()
	return e.getOrFail(t, dependency.PolicyBackend).(environment.DeployedPolicyBackend)
}

func (e *Environment) get(dep dependency.Instance) (interface{}, error) {
	s, ok := e.ctx.Tracker()[dep]
	if !ok {
		return nil, fmt.Errorf("dependency not initialized: %v", dep)
	}

	return s, nil
}

func (e *Environment) getOrFail(t testing.TB, dep dependency.Instance) interface{} {
	s, ok := e.ctx.Tracker()[dep]
	if !ok {
		t.Fatalf("Dependency not initialized: %v", dep)
	}
	return s
}

func newPilot() (environment.DeployedPilot, error) {
	// TODO(nmittler): We need a way to know whether or not mixer will be required.
	mesh := model.DefaultMeshConfig()

	args := pilot.Args{
		Namespace: namespace,
		Options: envoyv1.DiscoveryServiceOptions{
			HTTPAddr:       ":0",
			MonitoringAddr: ":0",
			GrpcAddr:       ":0",
			SecureGrpcAddr: ":0",
		},
		Mesh: &mesh,
	}
	return pilot.NewPilot(args)
}
