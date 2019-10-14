// Copyright 2019 Istio Authors.
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
	"io"
	"io/ioutil"

	"github.com/spf13/cobra"
	"istio.io/istio/pkg/kube"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd/api"
)

type Environment interface {
	GetConfig() *api.Config
	CreateClientSet(kubeconfig, context string) (kubernetes.Interface, error)
	Stdout() io.Writer
	Stderr() io.Writer
	ReadFile(filename string) ([]byte, error)
}

type kubeEnvironment struct {
	config *api.Config
	stdout io.Writer
	stderr io.Writer
}

func (e *kubeEnvironment) CreateClientSet(kubeconfig, context string) (kubernetes.Interface, error) {
	return kube.CreateClientset(kubeconfig, context)
}

func (e *kubeEnvironment) GetConfig() *api.Config                   { return e.config }
func (e *kubeEnvironment) Stdout() io.Writer                        { return e.stdout }
func (e *kubeEnvironment) Stderr() io.Writer                        { return e.stderr }
func (e *kubeEnvironment) ReadFile(filename string) ([]byte, error) { return ioutil.ReadFile(filename) }

func newKubeEnvFromCobra(kubeconfig, context string, cmd *cobra.Command) (Environment, error) {
	config, err := kube.BuildClientCmd(kubeconfig, context).ConfigAccess().GetStartingConfig()
	if err != nil {
		return nil, err
	}

	return &kubeEnvironment{
		config: config,
		stdout: cmd.OutOrStdout(),
		stderr: cmd.OutOrStderr(),
	}, nil
}
