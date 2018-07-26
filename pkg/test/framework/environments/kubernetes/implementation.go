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
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/go-multierror"
	"k8s.io/client-go/rest"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/internal"
	"istio.io/istio/pkg/test/framework/tmpl"
	"istio.io/istio/pkg/test/kube"
)

var scope = log.RegisterScope("testframework", "General scope for the test framework", 0)

// Implementation is the implementation of a kubernetes environment. It implements environment.Implementation,
// and also hosts publicly accessible methods that are specific to cluster environment.
type Implementation struct {
	ctx environment.ComponentContext

	Accessor *kube.Accessor

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

var _ internal.EnvironmentController = &Implementation{}
var _ environment.Implementation = &Implementation{}
var _ io.Closer = &Implementation{}

// New returns a new instance of cluster environment.
func New() *Implementation {
	return &Implementation{}
}

// EnvironmentName is the name of this environment implementation.
func (e *Implementation) EnvironmentName() string {
	return "kubernetes"
}

// Initialize the environment. This is called once during the lifetime of the suite.
func (e *Implementation) Initialize(ctx *internal.TestContext) error {
	config, err := kube.CreateConfig(ctx.Settings().KubeConfig)
	if err != nil {
		return err
	}

	if e.Accessor, err = kube.NewAccessor(config); err != nil {
		return err
	}

	e.ctx = ctx

	return e.allocateDependencyNamespace()
}

// Configure applies the given configuration to the mesh.
func (e *Implementation) Configure(config string) error {
	scope.Debugf("Applying configuration: \n%s\n", config)
	err := kube.ApplyContents(e.ctx.Settings().KubeConfig, e.TestNamespace, config)
	if err != nil {
		return err
	}

	// TODO: Implement a mechanism for reliably waiting for the configuration to disseminate in the system.
	// We can use CtrlZ to expose the config state of Mixer and Pilot.
	// See https://github.com/istio/istio/issues/6169 and https://github.com/istio/istio/issues/6170.
	time.Sleep(time.Second * 10)

	return nil
}

// Evaluate the template against standard set of parameters. See template.Parameters for details.
func (e *Implementation) Evaluate(template string) (string, error) {
	p := tmpl.Parameters{
		TestNamespace:       e.TestNamespace,
		DependencyNamespace: e.DependencyNamespace,
	}

	return tmpl.Evaluate(template, p)
}

// Reset the environment before starting another test.
func (e *Implementation) Reset() error {
	scope.Debug("Resetting environment")

	if err := e.deleteTestNamespace(); err != nil {
		return err
	}

	if err := e.allocateTestNamespace(); err != nil {
		return err
	}

	return nil
}

func (e *Implementation) deleteTestNamespace() error {
	if e.TestNamespace == "" {
		return nil
	}

	ns := e.TestNamespace
	err := e.Accessor.DeleteNamespace(ns)
	if err == nil {
		e.TestNamespace = ""

		// TODO: Waiting for deletion is taking a long time. This is probably not
		// needed for the general case. We should consider simply not doing this.
		// err = e.Accessor.WaitForNamespaceDeletion(ns)
	}

	return err
}

func (e *Implementation) allocateTestNamespace() error {
	ns := fmt.Sprintf("test-%s", uuid.New().String())

	err := e.Accessor.CreateNamespace(ns, "test-namespace")
	if err != nil {
		return err
	}

	e.TestNamespace = ns
	return nil
}

func (e *Implementation) allocateDependencyNamespace() error {
	ns := fmt.Sprintf("dep-%s", uuid.New().String())

	err := e.Accessor.CreateNamespace(ns, "dep-namespace")
	if err != nil {
		return err
	}

	e.DependencyNamespace = ns
	return nil
}

// Close implementation.
func (e *Implementation) Close() error {

	var err1 error
	var err2 error
	if e.TestNamespace != "" {
		err1 = e.Accessor.DeleteNamespace(e.TestNamespace)
		e.TestNamespace = ""
	}

	if e.DependencyNamespace != "" {
		err2 = e.Accessor.DeleteNamespace(e.DependencyNamespace)
		e.DependencyNamespace = ""
	}

	if err1 != nil || err2 != nil {
		return multierror.Append(err1, err2)
	}

	return nil
}
