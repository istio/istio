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

package cluster

import (
	"istio.io/istio/pkg/test/environment"
	"istio.io/istio/pkg/test/kube"
)

type deployedAPIServer struct {
	env      *Environment
	accessor *kube.Accessor
}

var _ environment.DeployedAPIServer = &deployedAPIServer{}

func newAPIServer(env *Environment) (*deployedAPIServer, error) {
	config, err := kube.CreateConfig(env.ctx.KubeConfigPath())
	if err != nil {
		return nil, err
	}
	accessor, err := kube.NewAccessor(config)
	if err != nil {
		return nil, err
	}

	return &deployedAPIServer{
		env:      env,
		accessor: accessor,
	}, nil
}

// ApplyYaml applys the given Yaml context against the target API Server.
func (a *deployedAPIServer) ApplyYaml(yml string) error {
	return kube.ApplyContents(a.env.ctx.KubeConfigPath(), a.env.TestNamespace, yml)
}
