// Copyright Istio Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package multicluster

import (
	"k8s.io/client-go/tools/clientcmd/api"

	"istio.io/istio/pkg/kube"
)

type ConditionFunc func() (done bool, err error)

type Environment interface {
	GetConfig() *api.Config
	CreateClient(context string) (kube.CLIClient, error)
}

type KubeEnvironment struct {
	config     *api.Config
	kubeconfig string
}

func (e *KubeEnvironment) CreateClient(context string) (kube.CLIClient, error) {
	cfg, err := kube.BuildClientConfig(e.kubeconfig, context)
	if err != nil {
		return nil, err
	}
	return kube.NewCLIClient(kube.NewClientConfigForRestConfig(cfg), "")
}

func (e *KubeEnvironment) GetConfig() *api.Config { return e.config }

var _ Environment = (*KubeEnvironment)(nil)

func NewEnvironment(kubeconfig, context string) (*KubeEnvironment, error) {
	config, err := kube.BuildClientCmd(kubeconfig, context).ConfigAccess().GetStartingConfig()
	if err != nil {
		return nil, err
	}

	return &KubeEnvironment{
		config:     config,
		kubeconfig: kubeconfig,
	}, nil
}
