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
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/clientcmd/api"

	"istio.io/istio/pkg/kube"
)

type ConditionFunc func() (done bool, err error)

type Environment interface {
	GetConfig() *api.Config
	CreateClient(context string) (kube.CLIClient, error)
	Stdout() io.Writer
	Stderr() io.Writer
	ReadFile(filename string) ([]byte, error)
	Printf(format string, a ...any)
	Errorf(format string, a ...any)
	Poll(interval, timeout time.Duration, condition ConditionFunc) error
}

type KubeEnvironment struct {
	config     *api.Config
	stdout     io.Writer
	stderr     io.Writer
	kubeconfig string
}

func (e *KubeEnvironment) CreateClient(context string) (kube.CLIClient, error) {
	cfg, err := kube.BuildClientConfig(e.kubeconfig, context)
	if err != nil {
		return nil, err
	}
	return kube.NewCLIClient(kube.NewClientConfigForRestConfig(cfg), "")
}

func (e *KubeEnvironment) Printf(format string, a ...any) {
	_, _ = fmt.Fprintf(e.stdout, format, a...)
}

func (e *KubeEnvironment) Errorf(format string, a ...any) {
	_, _ = fmt.Fprintf(e.stderr, format, a...)
}

func (e *KubeEnvironment) GetConfig() *api.Config                   { return e.config }
func (e *KubeEnvironment) Stdout() io.Writer                        { return e.stdout }
func (e *KubeEnvironment) Stderr() io.Writer                        { return e.stderr }
func (e *KubeEnvironment) ReadFile(filename string) ([]byte, error) { return os.ReadFile(filename) }
func (e *KubeEnvironment) Poll(interval, timeout time.Duration, condition ConditionFunc) error {
	return wait.Poll(interval, timeout, func() (bool, error) {
		return condition()
	})
}

var _ Environment = (*KubeEnvironment)(nil)

func NewEnvironment(kubeconfig, context string, stdout, stderr io.Writer) (*KubeEnvironment, error) {
	config, err := kube.BuildClientCmd(kubeconfig, context).ConfigAccess().GetStartingConfig()
	if err != nil {
		return nil, err
	}

	return &KubeEnvironment{
		config:     config,
		stdout:     stdout,
		stderr:     stderr,
		kubeconfig: kubeconfig,
	}, nil
}

func NewEnvironmentFromCobra(kubeconfig, context string, cmd *cobra.Command) (Environment, error) {
	return NewEnvironment(kubeconfig, context, cmd.OutOrStdout(), cmd.OutOrStderr())
}
