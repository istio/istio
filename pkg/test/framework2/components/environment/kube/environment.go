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

	"github.com/google/uuid"
	"istio.io/istio/pkg/test/framework2/components/environment"
	"istio.io/istio/pkg/test/framework2/resource"
	"istio.io/istio/pkg/test/framework2/runtime"
	"istio.io/istio/pkg/test/scopes"

	"istio.io/istio/pkg/test/kube"
)

//const (
//	validatingWebhookName = "istio-galley"
//)

const (
	// Name of the environment
	Name = "kube"
)

// Environment is the implementation of a kubernetes environment. It implements environment.Environment,
// and also hosts publicly accessible methods that are specific to cluster environment.
type Environment struct {
	*kube.Accessor
	s *settings
}

var _ environment.Instance = &Environment{}

// Name implements environment.Instance
func (e *Environment) Name() string {
	return Name
}

// New returns a new Kubernetes environment
func New(c environment.Context) (environment.Instance, error) {
	s, err := newSettingsFromCommandline()
	if err != nil {
		return nil, err
	}

	return newKube(s, c)
}

func newKube(s *settings, c environment.Context) (*Environment, error) {
	scopes.CI.Infof("Test Framework Kubernetes environment settings:\n%s", s)

	workDir, err := c.CreateTmpDirectory("kube")
	if err != nil {
		return nil, err
	}

	e := &Environment{}
	if e.Accessor, err = kube.NewAccessor(s.KubeConfig, workDir); err != nil {
		return nil, err
	}

	return e, nil
}

// NewNamespaceOrFail allocates a new testing namespace, or fails the test if it cannot be allocated.
func (e *Environment) NewNamespaceOrFail(s *runtime.TestContext, prefix string, inject bool) Namespace {
	s.T().Helper()
	n, err := e.NewNamespace(s, prefix, inject)
	if err != nil {
		s.T().Fatalf("error creating namespace with prefix %q: %v", prefix, err)
	}
	return n
}

// NewNamespace allocates a new testing namespace.
func (e *Environment) NewNamespace(s resource.Context, prefix string, inject bool) (Namespace, error) {
	ns := fmt.Sprintf("%s-%s", prefix, uuid.New().String()) // TODO: use RunID
	if err := e.Accessor.CreateNamespace(ns, "istio-test", inject); err != nil {
		return Namespace{}, err
	}

	n := Namespace{ns, e.Accessor}
	s.AddResource(n)

	return n, nil
}

// ApplyContents applies the given yaml contents to the namespace.
func (e *Environment) ApplyContents(ns Namespace, yml string) error {
	_, err := e.Accessor.ApplyContents(ns.ns, yml)
	return err
}
