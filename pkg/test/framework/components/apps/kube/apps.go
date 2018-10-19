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

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/kubernetes"
)

var (
	// Component provides a framework component for the local environment.
	Component = &kubeComponent{}

	deployments = []*deployment{
		{
			deployment:     "t",
			service:        "t",
			version:        "unversioned",
			port1:          8080,
			port2:          80,
			port3:          9090,
			port4:          90,
			port5:          7070,
			port6:          70,
			injectProxy:    false,
			headless:       false,
			serviceAccount: false,
		},
		{
			deployment:     "a",
			service:        "a",
			version:        "v1",
			port1:          8080,
			port2:          80,
			port3:          9090,
			port4:          90,
			port5:          7070,
			port6:          70,
			injectProxy:    true,
			headless:       false,
			serviceAccount: false,
		},
		{
			deployment:     "b",
			service:        "b",
			version:        "unversioned",
			port1:          80,
			port2:          8080,
			port3:          90,
			port4:          9090,
			port5:          70,
			port6:          7070,
			injectProxy:    true,
			headless:       false,
			serviceAccount: true,
		},
		{
			deployment:     "c-v1",
			service:        "c",
			version:        "v1",
			port1:          80,
			port2:          8080,
			port3:          90,
			port4:          9090,
			port5:          70,
			port6:          7070,
			injectProxy:    true,
			headless:       false,
			serviceAccount: true,
		},
		{
			deployment:     "c-v2",
			service:        "c",
			version:        "v2",
			port1:          80,
			port2:          8080,
			port3:          90,
			port4:          9090,
			port5:          70,
			port6:          7070,
			injectProxy:    true,
			headless:       false,
			serviceAccount: true,
		},
		{
			deployment:     "d",
			service:        "d",
			version:        "per-svc-auth",
			port1:          80,
			port2:          8080,
			port3:          90,
			port4:          9090,
			port5:          70,
			port6:          7070,
			injectProxy:    true,
			headless:       false,
			serviceAccount: true,
		},
		{
			deployment:     "headless",
			service:        "headless",
			version:        "unversioned",
			port1:          80,
			port2:          8080,
			port3:          90,
			port4:          9090,
			port5:          70,
			port6:          7070,
			injectProxy:    true,
			headless:       true,
			serviceAccount: true,
		},
	}
)

type kubeComponent struct {
}

// ID implements the component.Component interface.
func (c *kubeComponent) ID() dependency.Instance {
	return dependency.Apps
}

// Requires implements the component.Component interface.
func (c *kubeComponent) Requires() []dependency.Instance {
	return []dependency.Instance{
		dependency.Pilot,
	}
}

// Init implements the component.Component interface.
func (c *kubeComponent) Init(ctx environment.ComponentContext, deps map[dependency.Instance]interface{}) (interface{}, error) {
	e, ok := ctx.Environment().(*kubernetes.Implementation)
	if !ok {
		return nil, fmt.Errorf("unsupported environment: %q", ctx.Environment().EnvironmentID())
	}

	// Apply all the configs for the deployments.
	for _, d := range deployments {
		if err := d.apply(e); err != nil {
			return nil, multierror.Prefix(err, fmt.Sprintf("failed deploying %s: ", d.deployment))
		}
	}

	// Wait for the pods to transition to running.
	clients := make([]environment.DeployedApp, 0, len(deployments))
	for _, d := range deployments {
		if err := d.wait(e); err != nil {
			return nil, multierror.Prefix(err, fmt.Sprintf("failed waiting for deployment %s: ", d.deployment))
		}
		client, err := newClient(d.service, e)
		if err != nil {
			return nil, multierror.Prefix(err, fmt.Sprintf("failed creating client for deployment %s: ", d.deployment))
		}
		clients = append(clients, client)
	}

	return &appsImpl{
		e:       e,
		clients: clients,
	}, nil
}

type appsImpl struct {
	e       *kubernetes.Implementation
	clients []environment.DeployedApp
}

// GetApp implements the apps.Apps interface.
func (a *appsImpl) GetApp(name string) (environment.DeployedApp, error) {
	for _, c := range a.clients {
		if c.Name() == name {
			return c, nil
		}
	}

	return nil, fmt.Errorf("unable to locate app for name %s", name)
}
