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

package helm

import (
	"fmt"

	"istio.io/istio/pkg/test/shell"
)

const (
	// DefaultTillerServiceAccount is the default service account name for tiller.
	DefaultTillerServiceAccount = "tiller"

	// TillerPodName is the name of the pod that Helm uses when deploying tiller.
	TillerPodName = "tiller"
)

// DeployTiller deploys Helm's Tiller component. The default context from the Kube config file will be used.
func DeployTiller(kubeConfigPath, serviceAccount string) error {
	env := map[string]string{
		"KUBECONFIG": kubeConfigPath,
	}

	s, err := shell.ExecuteEnv(env, "helm init --upgrade --service-account %s", serviceAccount)
	if err == nil {
		return nil
	}

	return fmt.Errorf("%v: %s", err, s)
}

// Template calls "helm template".
func Template(chartName, namespace, chartDir, valuesFile string, settings map[string]string) (string, error) {
	valuesString := ""
	for k, v := range settings {
		valuesString += fmt.Sprintf(" --set %s=%s", k, v)
	}

	s, err := shell.Execute(
		"helm template %s --name %s --namespace %s --values %s%s",
		chartDir, chartName, namespace, valuesFile, valuesString)
	if err == nil {
		return s, nil
	}

	return "", fmt.Errorf("%v: %s", err, s)

}

// Install the chart from the given folder.
func Install(kubeConfigPath, chartDir, releaseName, namespace string, settings map[string]string) error {
	env := map[string]string{
		"KUBECONFIG": kubeConfigPath,
	}

	valuesString := ""
	for k, v := range settings {
		valuesString += fmt.Sprintf(" --set %s=%s", k, v)
	}

	s, err := shell.ExecuteEnv(env, "helm install %s --name %s --namespace %s%s",
		chartDir, releaseName, namespace, valuesString)
	if err == nil {
		return nil
	}

	return fmt.Errorf("%v: %s", err, s)
}

// InstallDryRun does a dry run of the installation of a chart from the given folder.
func InstallDryRun(kubeConfigPath, chartDir, releaseName, namespace string, settings map[string]string) error {
	env := map[string]string{
		"KUBECONFIG": kubeConfigPath,
	}

	settingsString := ""
	for k, v := range settings {
		settingsString += fmt.Sprintf(" --set %s=%s", k, v)
	}

	s, err := shell.ExecuteEnv(env, "helm install --dry-run --debug %s --name %s --namespace %s%s",
		chartDir, releaseName, namespace, settingsString)
	if err == nil {
		return nil
	}

	return fmt.Errorf("%v: %s", err, s)
}

// Delete a helm release
func Delete(kubeConfigPath, releaseName string) error {
	env := map[string]string{
		"KUBECONFIG": kubeConfigPath,
	}

	s, err := shell.ExecuteEnv(env, "helm delete %s", releaseName)
	if err == nil {
		return nil
	}

	return fmt.Errorf("%v: %s", err, s)
}
