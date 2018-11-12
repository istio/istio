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
	"strings"
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

const (
	validatingWebhookName = "istio-galley"
)

// Implementation is the implementation of a kubernetes environment. It implements environment.Implementation,
// and also hosts publicly accessible methods that are specific to cluster environment.
type Implementation struct {
	kube *Settings
	ctx  environment.ComponentContext

	Accessor   *kube.Accessor
	deployment *deployment.Instance

	// Both rest.Config and kube config path is used by different parts of the code.
	config *rest.Config

	systemNamespace     *namespace
	dependencyNamespace *namespace
	testNamespace       *namespace
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

	if e.Accessor, err = kube.NewAccessor(e.kube.KubeConfig, ctx.Settings().WorkDir); err != nil {
		return err
	}

	e.ctx = ctx

	// Create the namespace objects.
	e.systemNamespace = &namespace{
		name:             e.kube.IstioSystemNamespace,
		annotation:       "istio-system",
		accessor:         e.Accessor,
		injectionEnabled: false,
	}
	e.dependencyNamespace = &namespace{
		name:             e.kube.DependencyNamespace,
		annotation:       "istio-dep",
		accessor:         e.Accessor,
		injectionEnabled: true,
	}
	e.testNamespace = &namespace{
		name:             e.kube.TestNamespace,
		annotation:       "istio-test",
		accessor:         e.Accessor,
		injectionEnabled: false,
	}

	if err := e.systemNamespace.allocate(); err != nil {
		return err
	}
	if err := e.dependencyNamespace.allocate(); err != nil {
		return err
	}

	if e.kube.DeployIstio {
		if err := e.deployIstio(); err != nil {
			return err
		}
	}

	return nil
}

func (e *Implementation) deployIstio() (err error) {
	scopes.CI.Info("=== BEGIN: Deploy Istio (via Helm Template) ===")
	defer func() {
		if err != nil {
			scopes.CI.Infof("=== FAILED: Deploy Istio ===")
			scopes.CI.Infoa(err)
		} else {
			scopes.CI.Infof("=== SUCCEEDED: Deploy Istio ===")
		}
	}()

	e.deployment, err = deployment.NewHelmDeployment(deployment.HelmConfig{
		Accessor:   e.Accessor,
		Namespace:  e.systemNamespace.allocatedName,
		WorkDir:    e.ctx.Settings().WorkDir,
		ChartDir:   e.kube.ChartDir,
		ValuesFile: e.kube.ValuesFile,
		Values:     e.kube.Values,
	})
	if err == nil {
		err = e.deployment.Deploy(e.Accessor, true)
	}
	return
}

// Configure applies the given configuration to the mesh.
func (e *Implementation) Configure(config string) error {
	scopes.Framework.Debugf("Applying configuration: \n%s\n", config)
	err := e.Accessor.ApplyContents(e.testNamespace.allocatedName, config)
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
		IstioSystemNamespace: e.systemNamespace.allocatedName,
		TestNamespace:        e.testNamespace.allocatedName,
		DependencyNamespace:  e.dependencyNamespace.allocatedName,
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

// DumpState dumps the state of the environment to the file system and the log.
func (e *Implementation) DumpState(context string) {
	scopes.CI.Infof("=== BEGIN: Dump state (%s) ===", context)
	defer func() {
		scopes.CI.Infof("=== COMPLETED: Dump state (%s) ===", context)
	}()

	dir := path.Join(e.ctx.Settings().WorkDir, context)
	_, err := os.Stat(dir)
	if err != nil && os.IsNotExist(err) {
		err = os.Mkdir(dir, os.ModePerm)
	}

	if err != nil {
		scopes.Framework.Errorf("Unable to create folder to dump logs: %v", err)
		return
	}

	deployment.DumpPodData(dir, e.KubeSettings().IstioSystemNamespace, e.Accessor)
	deployment.DumpPodState(e.KubeSettings().IstioSystemNamespace, e.Accessor)

	if e.dependencyNamespace.allocatedName == "" {
		scopes.CI.Info("Skipping state dump of dependency namespace, as it is not allocated...")
	} else {
		deployment.DumpPodData(dir, e.dependencyNamespace.allocatedName, e.Accessor)
		deployment.DumpPodState(e.dependencyNamespace.allocatedName, e.Accessor)
	}

	if e.testNamespace.allocatedName == "" {
		scopes.CI.Info("Skipping state dump of test namespace, as it is not allocated...")
	} else {
		deployment.DumpPodData(dir, e.testNamespace.allocatedName, e.Accessor)
		deployment.DumpPodState(e.testNamespace.allocatedName, e.Accessor)
	}
}

// Close implementation.
func (e *Implementation) Close() error {
	var err error

	// Delete test and dependency namespaces if allocated.
	waitFuncs := make([]func() error, 0)
	for _, ns := range []*namespace{e.testNamespace, e.dependencyNamespace} {
		if ns.created {
			waitFunc, tempErr := ns.Close()
			if tempErr != nil {
				err = multierror.Append(err, tempErr)
			} else {
				waitFuncs = append(waitFuncs, waitFunc)
			}
		}
	}

	// Wait for any allocated namespaces to be deleted.
	for _, waitFunc := range waitFuncs {
		err = multierror.Append(err, waitFunc()).ErrorOrNil()
	}

	if e.KubeSettings().DeployIstio {
		// Delete the deployment yaml file.
		err = multierror.Append(e.deployment.Delete(e.Accessor, true))

		// Deleting the deployment should have removed everything, but just in case...

		// Make sure the system namespace is deleted.
		err = multierror.Append(err, e.systemNamespace.CloseAndWait()).ErrorOrNil()

		// Make sure the validating webhook is deleted.
		if e.Accessor.ValidatingWebhookConfigurationExists(validatingWebhookName) {
			scopes.CI.Info("Deleting ValidatingWebhook")
			err = multierror.Append(err, e.Accessor.DeleteValidatingWebhook(validatingWebhookName)).ErrorOrNil()
			err = multierror.Append(err, e.Accessor.WaitForValidatingWebhookDeletion(validatingWebhookName)).ErrorOrNil()
		}

		// Make sure all Istio CRDs are deleted.
		crds, tempErr := e.Accessor.GetCustomResourceDefinitions()
		if tempErr != nil {
			err = multierror.Append(err, tempErr)
		}
		if len(crds) > 0 {
			scopes.CI.Info("Deleting Istio CRDs")
			for _, crd := range crds {
				if strings.HasSuffix(crd.Name, ".istio.io") {
					err = multierror.Append(err, e.Accessor.DeleteCustomResourceDefinitions(crd.Name)).ErrorOrNil()
				}
			}
		}
	}

	return err
}

type namespace struct {
	name             string
	annotation       string
	allocatedName    string
	created          bool
	accessor         *kube.Accessor
	injectionEnabled bool
}

func (n *namespace) allocate() error {
	// Close if previously allocated
	err := n.CloseAndWait()
	if err != nil {
		return err
	}

	nameToAllocate := n.getNameToAllocate()

	// Only create the namespace if it doesn't already exist.
	if !n.accessor.NamespaceExists(nameToAllocate) {
		err := n.accessor.CreateNamespace(nameToAllocate, n.annotation, n.injectionEnabled)
		if err != nil {
			return err
		}
		n.created = true
	}

	n.allocatedName = nameToAllocate
	return nil
}

// Close implements io.Closer interface.
func (n *namespace) Close() (func() error, error) {
	if n.created && n.accessor.NamespaceExists(n.allocatedName) {
		scopes.CI.Infof("Deleting Namespace %s", n.allocatedName)
		defer func() {
			n.allocatedName = ""
			n.created = false
		}()
		name := n.allocatedName
		waitFunc := func() error {
			return n.accessor.WaitForNamespaceDeletion(name)
		}
		return waitFunc, n.accessor.DeleteNamespace(name)
	}
	return func() error { return nil }, nil
}

func (n *namespace) CloseAndWait() error {
	waitFunc, err := n.Close()
	if err == nil {
		err = waitFunc()
	}
	return err
}

func (n *namespace) getNameToAllocate() string {
	if n.name != "" {
		return n.name
	}
	return fmt.Sprintf("%s-%s", n.annotation, uuid.New().String())
}
