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

package kubernetes

import (
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	multierror "github.com/hashicorp/go-multierror"
	"k8s.io/client-go/rest"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/internal"
	"istio.io/istio/pkg/test/framework/tmpl"
	"istio.io/istio/pkg/test/kube"
)

var scope = log.RegisterScope("testframework", "General scope for the test framework", 0)

// Environment is a cluster-based environment for testing. It implements environment.Interface, and also
// hosts publicly accessible methods that are specific to cluster environment.
type Environment struct {
	ctx      *internal.TestContext
	accessor *kube.Accessor

	// Both rest.Config and kube config path is used by different parts of the code.
	config *rest.Config

	// The namespace where the Istio components reside in a typical deployment. This is typically
	// "istio-system" in a standard deployment.
	IstioSystemNamespace string

	// The namespace in which dependency components are deployed. This namespace is created once per run,
	// and does not get destroyed until all the tests run. Test framework dependencies can deploy
	// components here when they get initialized. They will get deployed only once.
	DependencyNamespace string

	// The namespace for each individual test. These namespaces are created when an environment is acquired
	// in a test, and the previous one gets deleted. This ensures that during a single test run, there is only
	// one test namespace in the system.
	TestNamespace string
}

var _ environment.Environment = &Environment{}
var _ internal.Environment = &Environment{}
var _ io.Closer = &Environment{}

// NewEnvironment returns a new instance of cluster environment.
func NewEnvironment() *Environment {

	return &Environment{}
}

// Initialize the environment. This is called once during the lifetime of the suite.
func (e *Environment) Initialize(ctx *internal.TestContext) error {
	config, err := kube.CreateConfig(ctx.KubeConfigPath())
	if err != nil {
		return err
	}

	accessor, err := kube.NewAccessor(config)
	if err != nil {
		return err
	}

	e.ctx = ctx
	e.config = config
	e.accessor = accessor

	return e.allocateDependencyNamespace()
}

// InitializeDependency is called when a new dependency is encountered during test run.
func (e *Environment) InitializeDependency(ctx *internal.TestContext, d dependency.Instance) (interface{}, error) {
	switch d {
	// Register ourselves as a resource to perform close-time cleanup.
	case dependency.Kube:
		return e, nil

	case dependency.PolicyBackend:
		return newPolicyBackend(e)

	case dependency.Mixer:
		return newMixer(e.ctx.KubeConfigPath(), e.accessor)

	case dependency.APIServer:
		return newAPIServer(e)

	default:
		return nil, fmt.Errorf("unrecognized dependency: %v", d)
	}
}

// Configure applies the given configuration to the mesh.
func (e *Environment) Configure(t testing.TB, config string) {
	t.Helper()
	scope.Debugf("Applying configuration: \n%s\n", config)
	err := kube.ApplyContents(e.ctx.KubeConfigPath(), e.TestNamespace, config)
	if err != nil {
		t.Fatalf("Error applying configuration: %v", err)
	}

	// TODO: Implement a mechanism for reliably waiting for the configuration to disseminate in the system.
	// We can use CtrlZ to expose the config state of Mixer and Pilot.
	// See https://github.com/istio/istio/issues/6169 and https://github.com/istio/istio/issues/6170.
	time.Sleep(time.Second * 10)
}

// Evaluate the template against standard set of parameters. See template.Parameters for details.
func (e *Environment) Evaluate(tb testing.TB, template string) string {
	p := tmpl.Parameters{
		TestNamespace:       e.TestNamespace,
		DependencyNamespace: e.DependencyNamespace,
	}

	s, err := tmpl.Evaluate(template, p)
	if err != nil {
		tb.Fatalf("Error evaluating template: %v", err)
	}

	return s
}

// Reset the environment before starting another test.
func (e *Environment) Reset() error {
	scope.Debug("Resetting environment")

	if err := e.deleteTestNamespace(); err != nil {
		return err
	}

	if err := e.allocateTestNamespace(); err != nil {
		return err
	}

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

// GetMixerOrFail returns a deployed Mixer instance in the framework, or fails the test if unsuccessful.
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
	s, err := e.get(dependency.Pilot)
	if err != nil {
		return nil, err
	}
	return s.(environment.DeployedPilot), nil
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

// GetAPIServer returns a handle to the ambient API Server in the environment.
func (e *Environment) GetAPIServer() (environment.DeployedAPIServer, error) {
	a, err := e.get(dependency.APIServer)
	if err != nil {
		return nil, err
	}

	return a.(environment.DeployedAPIServer), nil
}

// GetAPIServerOrFail returns a handle to the ambient API Server in the environment, or fails the test if
// unsuccessful.
func (e *Environment) GetAPIServerOrFail(t testing.TB) environment.DeployedAPIServer {
	t.Helper()

	m, err := e.GetAPIServer()
	if err != nil {
		t.Fatal(err)
	}

	return m
}

// GetApp returns a fake testing app object for the given name.
func (e *Environment) GetApp(name string) (environment.DeployedApp, error) {
	return getApp(name, e.TestNamespace)
}

// GetAppOrFail returns a fake testing app object for the given name, or fails the test if unsuccessful.
func (e *Environment) GetAppOrFail(name string, t testing.TB) environment.DeployedApp {
	t.Helper()
	a, err := getApp(name, e.TestNamespace)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// GetFortioApp returns a Fortio App object for the given name.
func (e *Environment) GetFortioApp(name string) (environment.DeployedFortioApp, error) {
	// TODO: Implement or remove the method
	// See https://github.com/istio/istio/issues/6171
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

func (e *Environment) deleteTestNamespace() error {
	if e.TestNamespace == "" {
		return nil
	}

	ns := e.TestNamespace
	err := e.accessor.DeleteNamespace(ns)
	if err == nil {
		e.TestNamespace = ""

		// TODO: Waiting for deletion is taking a long time. This is probably not
		// needed for the general case. We should consider simply not doing this.
		// err = e.accessor.WaitForNamespaceDeletion(ns)
	}

	return err
}

func (e *Environment) allocateTestNamespace() error {
	ns := fmt.Sprintf("test-%s", uuid.New().String())

	err := e.accessor.CreateNamespace(ns, "test-namespace")
	if err != nil {
		return err
	}

	e.TestNamespace = ns
	return nil
}

func (e *Environment) allocateDependencyNamespace() error {
	ns := fmt.Sprintf("dep-%s", uuid.New().String())

	err := e.accessor.CreateNamespace(ns, "dep-namespace")
	if err != nil {
		return err
	}

	e.DependencyNamespace = ns
	return nil
}

// Close implementation.
func (e *Environment) Close() error {

	var err1 error
	var err2 error
	if e.TestNamespace != "" {
		err1 = e.accessor.DeleteNamespace(e.TestNamespace)
		e.TestNamespace = ""
	}

	if e.DependencyNamespace != "" {
		err2 = e.accessor.DeleteNamespace(e.DependencyNamespace)
		e.DependencyNamespace = ""
	}

	if err1 != nil || err2 != nil {
		return multierror.Append(err1, err2)
	}

	return nil
}
