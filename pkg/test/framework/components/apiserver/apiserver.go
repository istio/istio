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

package apiserver

import (
	"fmt"

	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/kubernetes"
	"istio.io/istio/pkg/test/kube"
)

// KubeComponent is a framework component for the Kubernetes API server.
var KubeComponent = &kubeComponent{}

type kubeComponent struct {
}

// ID implements the component.Component interface.
func (c *kubeComponent) ID() dependency.Instance {
	return dependency.APIServer
}

// Requires implements the component.Component interface.
func (c *kubeComponent) Requires() []dependency.Instance {
	return make([]dependency.Instance, 0)
}

// Init implements the component.Component interface.
func (c *kubeComponent) Init(ctx environment.ComponentContext, deps map[dependency.Instance]interface{}) (interface{}, error) {
	kubeEnv, ok := ctx.Environment().(*kubernetes.Implementation)
	if !ok {
		return nil, fmt.Errorf("unsupported environment: %q", ctx.Environment().EnvironmentID())
	}

	return &deployedAPIServer{
		ctx: ctx,
		env: kubeEnv,
	}, nil
}

type deployedAPIServer struct {
	ctx environment.ComponentContext
	env *kubernetes.Implementation
}

var _ environment.DeployedAPIServer = &deployedAPIServer{}

// ApplyYaml applies the given Yaml context against the target API Server.
func (a *deployedAPIServer) ApplyYaml(yml string) error {
	s := a.env.KubeSettings()
	return kube.ApplyContents(s.KubeConfig, s.TestNamespace, yml)
}
