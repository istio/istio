//  Copyright Istio Authors
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

package helm

import (
	"fmt"
	"strings"
	"time"

	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/shell"
)

// Helm allows clients to interact with helm commands in their cluster
type Helm struct {
	kubeConfig string
}

// New returns a new instance of a helm object.
func New(kubeConfig string) *Helm {
	return &Helm{
		kubeConfig: kubeConfig,
	}
}

// InstallChartWithValues installs the specified chart with its given name to the given namespace
func (h *Helm) InstallChartWithValues(name, chartPath, namespace string, values []string, timeout time.Duration) error {
	command := fmt.Sprintf("helm install %s %s --namespace %s --kubeconfig %s --timeout %s %s",
		name, chartPath, namespace, h.kubeConfig, timeout, strings.Join(values, " "))
	_, err := execCommand(command)
	return err
}

// InstallChart installs the specified chart with its given name to the given namespace
func (h *Helm) InstallChart(name, chartPath, namespace, overridesFile string, timeout time.Duration) error {
	command := fmt.Sprintf("helm install %s %s --namespace %s -f %s --kubeconfig %s --timeout %s",
		name, chartPath, namespace, overridesFile, h.kubeConfig, timeout)
	_, err := execCommand(command)
	return err
}

// UpgradeChart upgrades the specified chart with its given name to the given namespace; does not use baseWorkDir
// but the full path passed
func (h *Helm) UpgradeChart(name, chartPath, namespace, overridesFile string, timeout time.Duration, args ...string) error {
	command := fmt.Sprintf("helm upgrade %s %s --namespace %s -f %s --kubeconfig %s --timeout %s %v",
		name, chartPath, namespace, overridesFile, h.kubeConfig, timeout, strings.Join(args, " "))
	_, err := execCommand(command)
	return err
}

// DeleteChart deletes the specified chart with its given name in the given namespace
func (h *Helm) DeleteChart(name, namespace string) error {
	command := fmt.Sprintf("helm delete %s --namespace %s --kubeconfig %s", name, namespace, h.kubeConfig)
	_, err := execCommand(command)
	return err
}

// Template runs the template command and applies the generated file with kubectl
func (h *Helm) Template(name, chartPath, namespace, templateFile string, timeout time.Duration, args ...string) (string, error) {
	command := fmt.Sprintf("helm template %s %s --namespace %s -s %s --kubeconfig %s --timeout %s %s ",
		name, chartPath, namespace, templateFile, h.kubeConfig, timeout, strings.Join(args, " "))

	return execCommand(command)
}

func execCommand(cmd string) (string, error) {
	scopes.Framework.Infof("Applying helm command: %s", cmd)

	s, err := shell.Execute(true, cmd)
	if err != nil {
		scopes.Framework.Infof("(FAILED) Executing helm: %s (err: %v): %s", cmd, err, s)
		return "", fmt.Errorf("%v: %s", err, s)
	}

	return s, nil
}
