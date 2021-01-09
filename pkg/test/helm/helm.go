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
	"path/filepath"
	"strings"
	"time"

	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/shell"
)

// Helm allows clients to interact with helm commands in their cluster
type Helm struct {
	kubeConfig string
	baseDir    string
}

// New returns a new instance of a helm object.
func New(kubeConfig, baseWorkDir string) *Helm {
	return &Helm{
		kubeConfig: kubeConfig,
		baseDir:    baseWorkDir,
	}
}

// InstallChart installs the specified chart with its given name to the given namespace
func (h *Helm) InstallChartWithValues(name, relpath, namespace string, values []string, timeout time.Duration) error {
	p := filepath.Join(h.baseDir, relpath)

	command := fmt.Sprintf("helm install %s %s --namespace %s --kubeconfig %s --timeout %s %s",
		name, p, namespace, h.kubeConfig, timeout, strings.Join(values, " "))
	return execCommand(command)
}

// InstallChart installs the specified chart with its given name to the given namespace
func (h *Helm) InstallChart(name, relpath, namespace, overridesFile string, timeout time.Duration) error {
	p := filepath.Join(h.baseDir, relpath)
	command := fmt.Sprintf("helm install %s %s --namespace %s -f %s --kubeconfig %s --timeout %s",
		name, p, namespace, overridesFile, h.kubeConfig, timeout)
	return execCommand(command)
}

// UpgradeChart upgrades the specified chart with its given name to the given namespace; does not use baseWorkDir
// but the full path passed
func (h *Helm) UpgradeChart(name, chartPath, namespace, overridesFile string, timeout time.Duration) error {
	command := fmt.Sprintf("helm upgrade %s %s --namespace %s -f %s --kubeconfig %s --timeout %s",
		name, chartPath, namespace, overridesFile, h.kubeConfig, timeout)
	return execCommand(command)
}

// DeleteChart deletes the specified chart with its given name in the given namespace
func (h *Helm) DeleteChart(name, namespace string) error {
	command := fmt.Sprintf("helm delete %s --namespace %s --kubeconfig %s", name, namespace, h.kubeConfig)
	return execCommand(command)
}

func execCommand(cmd string) error {
	scopes.Framework.Infof("Applying helm command: %s", cmd)

	s, err := shell.Execute(true, cmd)
	if err != nil {
		scopes.Framework.Infof("(FAILED) Executing helm: %s (err: %v): %s", cmd, err, s)
		return fmt.Errorf("%v: %s", err, s)
	}

	return nil
}
