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
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework2/core"

	"istio.io/istio/pkg/test/deployment"

	"github.com/google/uuid"

	"istio.io/istio/pkg/test/scopes"

	"istio.io/istio/pkg/test/kube"
)

// Environment is the implementation of a kubernetes environment. It implements environment.Environment,
// and also hosts publicly accessible methods that are specific to cluster environment.
type Environment struct {
	id core.ResourceID

	ctx core.Context
	*kube.Accessor
	s *Settings
}

var _ core.Environment = &Environment{}

// New returns a new Kubernetes environment
func New(ctx core.Context) (core.Environment, error) {
	s, err := newSettingsFromCommandline()
	if err != nil {
		return nil, err
	}

	scopes.CI.Infof("Test Framework Kubernetes environment Settings:\n%s", s)

	workDir, err := ctx.CreateTmpDirectory("kube")
	if err != nil {
		return nil, err
	}

	e := &Environment{
		ctx: ctx,
		s:   s,
	}
	e.id = ctx.TrackResource(e)

	if e.Accessor, err = kube.NewAccessor(s.KubeConfig, workDir); err != nil {
		return nil, err
	}

	return e, nil
}

// EnvironmentName implements environment.Instance
func (e *Environment) EnvironmentName() core.EnvironmentName {
	return core.Kube
}

// EnvironmentName implements environment.Instance
func (e *Environment) Case(name core.EnvironmentName, fn func()) {
	if name == e.EnvironmentName() {
		fn()
	}
}

// ID implements resource.Instance
func (e *Environment) ID() core.ResourceID {
	return e.id
}

func (e *Environment) Settings() *Settings {
	return e.s.clone()
}

func (e *Environment) ClaimNamespace(name string) (core.Namespace, error) {
	if !e.Accessor.NamespaceExists(name) {
		if err := e.Accessor.CreateNamespace(name, "istio-test", true); err != nil {
			return nil, err
		}

	}
	return &kubeNamespace{name: name}, nil
}

func (e *Environment) ClaimNamespaceOrFail(t *testing.T, name string) core.Namespace {
	ns, err := e.ClaimNamespace(name)
	if err != nil {
		t.Fatalf("ClaimNamespaceOrFail: %v", err)
	}

	return ns
}

// NewNamespace allocates a new testing namespace.
func (e *Environment) NewNamespace(ctx core.Context, prefix string, inject bool) (core.Namespace, error) {
	ns := fmt.Sprintf("%s-%s", prefix, uuid.New().String())
	if err := e.Accessor.CreateNamespace(ns, "istio-test", inject); err != nil {
		return nil, err
	}

	n := &kubeNamespace{name: ns, a: e.Accessor}
	id := ctx.TrackResource(n)
	n.id = id

	return n, nil
}

// NewNamespaceOrFail allocates a new testing namespace, or fails the test if it cannot be allocated.
func (e *Environment) NewNamespaceOrFail(t *testing.T, ctx core.Context, prefix string, inject bool) core.Namespace {
	t.Helper()
	n, err := e.NewNamespace(ctx, prefix, inject)
	if err != nil {
		t.Fatalf("Environment.NewNamespaceOrFail: %v", err)
	}
	return n
}

// ApplyContents applies the given yaml contents to the namespace.
func (e *Environment) ApplyContents(ns core.Namespace, yml string) error {
	_, err := e.Accessor.ApplyContents(ns.Name(), yml)
	return err
}

func (e *Environment) DeployYaml(namespace, yamlFile string) (*deployment.Instance, error) {
	i := deployment.NewYamlDeployment(namespace, yamlFile)

	err := i.Deploy(e.Accessor, true)
	if err != nil {
		return nil, err
	}
	return i, nil
}
