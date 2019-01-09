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

package kube

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"text/template"

	"github.com/google/uuid"
	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/deployment"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	validatingWebhookName = "istio-galley"
)

var _ api.Environment = &Environment{}
var _ api.Resettable = &Environment{}
var _ io.Closer = &Environment{}

// Environment is the implementation of a kubernetes environment. It implements environment.Environment,
// and also hosts publicly accessible methods that are specific to cluster environment.
type Environment struct {
	*kube.Accessor

	ctx context.Instance

	s               *settings
	scope           lifecycle.Scope
	systemNamespace *namespace
	suiteNamespace  *namespace
	testNamespace   *namespace
	deployment      *deployment.Instance
}

// NewEnvironment factory function for the component
func NewEnvironment() (api.Component, error) {
	return &Environment{}, nil
}

// GetEnvironment from the repository.
func GetEnvironment(r component.Repository) (*Environment, error) {
	e := api.GetEnvironment(r)
	if e == nil {
		return nil, fmt.Errorf("environment has not been created")
	}

	ne, ok := e.(*Environment)
	if !ok {
		return nil, fmt.Errorf("unsupported environment: %q", e.Descriptor().Variant)
	}
	return ne, nil
}

// GetEnvironmentOrFail from the repository.
func GetEnvironmentOrFail(r component.Repository, t testing.TB) *Environment {
	e, err := GetEnvironment(r)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// Scope for this component.
func (e *Environment) Scope() lifecycle.Scope {
	return e.scope
}

// Descriptor for this component
func (e *Environment) Descriptor() component.Descriptor {
	return descriptors.KubernetesEnvironment
}

// SystemNamespace namespace used for components deployed to the Istio system.
func (e *Environment) SystemNamespace() string {
	return e.systemNamespace.allocatedName
}

// SuiteNamespace namespace used for non-system components that have Suite scope.
func (e *Environment) SuiteNamespace() string {
	return e.suiteNamespace.allocatedName
}

// TestNamespace namespace used for non-system components that have Test scope.
func (e *Environment) TestNamespace() string {
	return e.testNamespace.allocatedName
}

// NamespaceForScope returns the namespace to be used for the given scope.
func (e *Environment) NamespaceForScope(scope lifecycle.Scope) string {
	switch scope {
	case lifecycle.System:
		return e.SystemNamespace()
	case lifecycle.Suite:
		return e.SuiteNamespace()
	default:
		return e.TestNamespace()
	}
}

// DeployIstio indicates whether the Istio system should be deployed.
func (e *Environment) DeployIstio() bool {
	return e.s.DeployIstio
}

// MinikubeIngress indicates that the Ingress Gateway is not available. This typically happens in Minikube. The Ingress
// component will fall back to node-port in this case.
func (e *Environment) MinikubeIngress() bool {
	return e.s.MinikubeIngress
}

// HelmValueMap returns the overrides for helm values.
func (e *Environment) HelmValueMap() map[string]string {
	out := make(map[string]string)
	for k, v := range e.s.Values {
		out[k] = v
	}
	return out
}

// Start implements the api.Environment interface
func (e *Environment) Start(ctx context.Instance, scope lifecycle.Scope) error {
	e.scope = scope

	var err error
	e.s, err = newSettings()
	if err != nil {
		return err
	}

	scopes.CI.Infof("Test Framework Kubernetes environment settings:\n%s", e.s)

	if e.Accessor, err = kube.NewAccessor(e.s.KubeConfig, ctx.WorkDir()); err != nil {
		return err
	}

	e.ctx = ctx

	// Create the namespace objects.
	e.systemNamespace = &namespace{
		name:             e.s.SystemNamespace,
		annotation:       "istio-system",
		accessor:         e.Accessor,
		injectionEnabled: false,
	}
	e.suiteNamespace = &namespace{
		name:             e.s.SuiteNamespace,
		annotation:       "istio-suite",
		accessor:         e.Accessor,
		injectionEnabled: true,
	}
	e.testNamespace = &namespace{
		name:             e.s.TestNamespace,
		annotation:       "istio-test",
		accessor:         e.Accessor,
		injectionEnabled: false,
	}

	if err := e.systemNamespace.allocate(); err != nil {
		return err
	}
	if err := e.suiteNamespace.allocate(); err != nil {
		return err
	}
	if err := e.testNamespace.allocate(); err != nil {
		return err
	}

	if e.s.DeployIstio {
		if err := e.deployIstio(); err != nil {
			e.DumpState(ctx.TestID())
			return err
		}
	}

	return nil
}

func (e *Environment) deployIstio() (err error) {
	scopes.CI.Info("=== BEGIN: Deploy Istio (via Helm Template) ===")
	defer func() {
		if err != nil {
			scopes.CI.Infof("=== FAILED: Deploy Istio ===")
		} else {
			scopes.CI.Infof("=== SUCCEEDED: Deploy Istio ===")
		}
	}()

	e.deployment, err = deployment.NewHelmDeployment(deployment.HelmConfig{
		Accessor:   e.Accessor,
		Namespace:  e.systemNamespace.allocatedName,
		WorkDir:    e.ctx.WorkDir(),
		ChartDir:   e.s.ChartDir,
		ValuesFile: e.s.ValuesFile,
		Values:     e.s.Values,
	})
	if err == nil {
		err = e.deployment.Deploy(e.Accessor, true, retry.Timeout(e.s.DeployTimeout))
	}
	return
}

// Evaluate the template against standard set of parameters. See template.parameters for details.
func (e *Environment) Evaluate(template string) (string, error) {
	p := parameters{
		SystemNamespace: e.SystemNamespace(),
		TestNamespace:   e.TestNamespace(),
		SuiteNamespace:  e.SuiteNamespace(),
	}

	return e.EvaluateWithParams(template, p)
}

// DeployYaml deploys the given yaml with the given scope.
func (e *Environment) DeployYaml(yamlFile string, scope lifecycle.Scope) (*deployment.Instance, error) {
	i := deployment.NewYamlDeployment(e.NamespaceForScope(scope), yamlFile)

	err := i.Deploy(e.Accessor, true)
	if err != nil {
		return nil, err
	}
	return i, nil
}

// EvaluateWithParams the given template using the provided data.
func (e *Environment) EvaluateWithParams(tpl string, data interface{}) (string, error) {
	t := template.New("test template")

	t2, err := t.Parse(tpl)
	if err != nil {
		return "", err
	}

	var b bytes.Buffer
	if err = t2.Execute(&b, data); err != nil {
		return "", err
	}

	return b.String(), nil
}

// DumpState dumps the state of the environment to the file system and the log.
func (e *Environment) DumpState(context string) {
	scopes.CI.Infof("=== BEGIN: Dump state (%s) ===", context)
	defer func() {
		scopes.CI.Infof("=== COMPLETED: Dump state (%s) ===", context)
	}()

	dir, err := e.ctx.CreateTmpDirectory(context)
	if err != nil {
		scopes.Framework.Errorf("Unable to create folder to dump logs: %v", err)
		return
	}

	scopes.CI.Infof("Dumping system state to: %s", dir)

	e.dumpStateForNamespace(e.SystemNamespace(), dir)
	e.dumpStateForNamespace(e.SuiteNamespace(), dir)
	e.dumpStateForNamespace(e.TestNamespace(), dir)
}

func (e *Environment) dumpStateForNamespace(ns, dir string) {
	if ns == "" {
		scopes.CI.Infof("Skipping state dump for namespace `%s`, as it is not allocated...", ns)
	} else {
		scopes.CI.Infof("Dumping state for namespace `%s`", ns)
		deployment.DumpPodData(dir, ns, e.Accessor)
		deployment.DumpPodState(ns, e.Accessor)
	}
}

// Close implementation.
func (e *Environment) Close() (err error) {
	// Delete test and dependency namespaces if allocated.
	waitFuncs := make([]func() error, 0)
	for _, ns := range []*namespace{e.testNamespace, e.suiteNamespace} {
		if ns.created {
			waitFunc, e := ns.Close()
			if e != nil {
				err = multierror.Append(err, e)
			} else {
				waitFuncs = append(waitFuncs, waitFunc)
			}
		}
	}

	// Wait for any allocated namespaces to be deleted.
	for _, waitFunc := range waitFuncs {
		err = multierror.Append(err, waitFunc()).ErrorOrNil()
	}

	if e.deployment != nil {
		// Delete the deployment yaml file.
		err = multierror.Append(err, e.deployment.Delete(e.Accessor, true, retry.Timeout(e.s.UndeployTimeout))).ErrorOrNil()
		e.deployment = nil

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

// Reset the environment before starting another test.
func (e *Environment) Reset() error {
	scopes.Framework.Debug("Resetting environment")

	// Re-allocate the test namespace.
	if err := e.testNamespace.allocate(); err != nil {
		return err
	}

	return nil
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
