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
	"os"
	"path"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/go-multierror"
	"k8s.io/client-go/rest"

	"istio.io/istio/pkg/test/deployment"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/internal"
	"istio.io/istio/pkg/test/framework/scopes"
	"istio.io/istio/pkg/test/framework/settings"
	"istio.io/istio/pkg/test/framework/tmpl"
	"istio.io/istio/pkg/test/kube"
)

// Implementation is the implementation of a kubernetes environment. It implements environment.Implementation,
// and also hosts publicly accessible methods that are specific to cluster environment.
type Implementation struct {
	kube *Settings
	ctx  environment.ComponentContext

	Accessor *kube.Accessor

	// Both rest.Config and kube config path is used by different parts of the code.
	config *rest.Config

	systemNamespace     *namespace
	dependencyNamespace *namespace
	testNamespace       *namespace

	deployment *deployment.Instance
}

var _ internal.EnvironmentController = &Implementation{}
var _ environment.Implementation = &Implementation{}
var _ io.Closer = &Implementation{}

// New returns a new instance of cluster environment.
func New() *Implementation {
	return &Implementation{}
}

// KubeSettings for this environment.
func (e *Implementation) KubeSettings() *Settings {
	// Copy the settings.
	s := &(*e.kube)

	// Overwrite the namespaces with the allocated name.
	s.IstioSystemNamespace = e.systemNamespace.allocatedName
	s.DependencyNamespace = e.dependencyNamespace.allocatedName
	s.TestNamespace = e.testNamespace.allocatedName
	return s
}

// EnvironmentID is the name of this environment implementation.
func (e *Implementation) EnvironmentID() settings.EnvironmentID {
	return settings.Kubernetes
}

// Initialize the environment. This is called once during the lifetime of the suite.
func (e *Implementation) Initialize(ctx *internal.TestContext) error {
	var err error
	e.kube, err = newSettings()
	if err != nil {
		return err
	}

	scopes.CI.Infof("Test Framework Kubernetes environment settings:\n%s", e.kube)

	config, err := kube.CreateConfig(e.kube.KubeConfig)
	if err != nil {
		return err
	}

	if e.Accessor, err = kube.NewAccessor(config); err != nil {
		return err
	}

	e.ctx = ctx

	// Create the namespace objects.
	e.systemNamespace = &namespace{
		name:       e.kube.IstioSystemNamespace,
		annotation: "system-namespace",
		accessor:   e.Accessor,
	}
	e.dependencyNamespace = &namespace{
		name:       e.kube.DependencyNamespace,
		annotation: "dep-namespace",
		accessor:   e.Accessor,
	}
	e.testNamespace = &namespace{
		name:       e.kube.TestNamespace,
		annotation: "test-namespace",
		accessor:   e.Accessor,
	}

	if err := e.systemNamespace.allocate(); err != nil {
		return err
	}
	if err := e.dependencyNamespace.allocate(); err != nil {
		return err
	}

	if e.kube.DeployIstio {
		goDir := os.Getenv("GOPATH")
		chartsDir := path.Join(goDir, "src/istio.io/istio/install/kubernetes/helm")

		// TODO: Values files should be parameterized.
		e.deployment, err = deployment.Start(
			e.kube.KubeConfig,
			chartsDir,
			ctx.Settings().WorkDir,
			e.kube.Hub,
			e.kube.Tag,
			e.systemNamespace.allocatedName,
			deployment.IstioMCP)

		if err == nil {
			err = e.deployment.Wait(e.Accessor)
		}

		if err != nil {
			deployment.DumpPodState(e.kube.KubeConfig, e.systemNamespace.allocatedName)
			deployment.CopyPodLogs(e.kube.KubeConfig, ctx.Settings().WorkDir, e.systemNamespace.allocatedName, e.Accessor)
			return err
		}
	}

	return nil
}

// Configure applies the given configuration to the mesh.
func (e *Implementation) Configure(config string) error {
	scopes.Framework.Debugf("Applying configuration: \n%s\n", config)
	err := kube.ApplyContents(e.kube.KubeConfig, e.systemNamespace.allocatedName, config)
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
		TestNamespace:       e.testNamespace.allocatedName,
		DependencyNamespace: e.dependencyNamespace.allocatedName,
	}

	return tmpl.Evaluate(template, p)
}

// Reset the environment before starting another test.
func (e *Implementation) Reset() error {
	scopes.Framework.Debug("Resetting environment")

	// Re-allocate the test namespace.
	if err := e.testNamespace.allocate(); err != nil {
		return err
	}

	return nil
}

// Close implementation.
func (e *Implementation) Close() error {
	var err error
	for _, ns := range []*namespace{e.testNamespace, e.dependencyNamespace, e.systemNamespace} {
		if e := ns.Close(); e != nil {
			err = multierror.Append(err, e)
		}
	}

	if e.deployment != nil {
		if err2 := e.deployment.Delete(); err2 != nil {
			err = multierror.Append(err, err2)
		}
		e.deployment = nil
	}

	return err
}

type namespace struct {
	name          string
	annotation    string
	allocatedName string
	created       bool
	accessor      *kube.Accessor
}

func (n *namespace) allocate() error {
	// Close if previously allocated
	if err := n.Close(); err != nil {
		return err
	}

	nameToAllocate := n.getNameToAllocate()

	// Only create the namespace if it doesn't already exist.
	if !n.accessor.NamespaceExists(nameToAllocate) {
		err := n.accessor.CreateNamespace(nameToAllocate, n.annotation)
		if err != nil {
			return err
		}
		n.created = true
	}

	n.allocatedName = nameToAllocate
	return nil
}

// Close implements io.Closer.Close.
func (n *namespace) Close() error {
	if n.created {
		defer func() {
			n.allocatedName = ""
			n.created = false
		}()
		return n.accessor.DeleteNamespace(n.allocatedName)
	}
	return nil
}

func (n *namespace) getNameToAllocate() string {
	if n.name != "" {
		return n.name
	}
	return fmt.Sprintf("%s-%s", n.annotation, uuid.New().String())
}
