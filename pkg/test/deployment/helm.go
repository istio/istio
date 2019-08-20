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

package deployment

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pkg/errors"

	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/shell"
)

/*
const (
	namespaceTemplate = `apiVersion: v1
kind: Namespace
metadata:
  name: %s
  labels:
    istio-injection: disabled
`
	zeroCRDInstallFile        = "crd-10.yaml"
	oneCRDInstallFile         = "crd-11.yaml"
	twoCRDInstallFile         = "crd-12.yaml"
	certManagerCRDInstallFile = "crd-certmanager-10.yaml"
)
*/
// HelmConfig configuration for a Helm-based deployment.
type HelmConfig struct {
	Accessor     *kube.Accessor
	Namespace    string
	WorkDir      string
	ChartDir     string
	CrdsFilesDir string

	// Can be either a file name under ChartDir or an absolute file path.
	ValuesFile string
	Values     map[string]string
}

/*
// NewHelmDeployment creates a new Helm-based deployment instance.
func NewHelmDeployment(c HelmConfig) (*Instance, error) {
	// Define a deployment name for Helm.
	deploymentName := fmt.Sprintf("%s-%v", c.Namespace, time.Now().UnixNano())
	scopes.CI.Infof("Generated Helm Instance name: %s", deploymentName)

	yamlFilePath := path.Join(c.WorkDir, deploymentName+".yaml")

	// Convert the valuesFile to an absolute file path.
	valuesFile := c.ValuesFile
	if _, err := os.Stat(valuesFile); os.IsNotExist(err) {
		valuesFile = filepath.Join(c.ChartDir, valuesFile)
		if _, err := os.Stat(valuesFile); os.IsNotExist(err) {
			return nil, err
		}
	}

	generatedYaml, err := HelmTemplate(
		deploymentName,
		c.Namespace,
		c.ChartDir,
		c.WorkDir,
		valuesFile,
		c.Values)

	if err != nil {
		return nil, fmt.Errorf("chart generation failed: %v", err)
	}

	// TODO: This is Istio deployment specific. We may need to remove/reconcile this as a parameter
	// when we support Helm deployment of non-Istio artifacts.
	namespaceData := fmt.Sprintf(namespaceTemplate, c.Namespace)
	crdsData, err := getCrdsYamlFiles(c)
	if err != nil {
		return nil, err
	}
	generatedYaml = test.JoinConfigs(namespaceData, crdsData, generatedYaml)

	if err = ioutil.WriteFile(yamlFilePath, []byte(generatedYaml), os.ModePerm); err != nil {
		return nil, fmt.Errorf("unable to write helm generated yaml: %v", err)
	}

	scopes.CI.Infof("Created Helm-generated Yaml file: %s", yamlFilePath)
	return NewYamlDeployment(c.Namespace, yamlFilePath), nil
}

func getCrdsYamlFiles(c HelmConfig) (string, error) {
	// Note: When adding a CRD to the install, a new CRDFile* constant is needed
	// This slice contains the list of CRD files installed during testing
	istioCRDFileNames := []string{zeroCRDInstallFile, oneCRDInstallFile, twoCRDInstallFile, certManagerCRDInstallFile}
	// Get Joined Crds Yaml file
	prevContent := ""
	for _, yamlFileName := range istioCRDFileNames {
		content, err := test.ReadConfigFile(path.Join(c.CrdsFilesDir, yamlFileName))
		if err != nil {
			return "", err
		}
		prevContent = test.JoinConfigs(content, prevContent)
	}
	return prevContent, nil
}
*/

// HelmTemplate calls "helm template".
func HelmTemplate(deploymentName, namespace, chartDir, workDir, valuesFile string, values map[string]string) (string, error) {
	// Apply the overrides for the values file.
	valuesString := ""
	for k, v := range values {
		valuesString += fmt.Sprintf(" --set %s=%s", k, v)
	}

	valuesFileString := ""
	if valuesFile != "" {
		valuesFileString = fmt.Sprintf("--values %s", valuesFile)
	}

	helmRepoDir := filepath.Join(workDir, "helmrepo")
	chartBuildDir := filepath.Join(workDir, "charts")
	if err := os.MkdirAll(helmRepoDir, os.ModePerm); err != nil {
		return "", err
	}
	if err := os.MkdirAll(chartBuildDir, os.ModePerm); err != nil {
		return "", err
	}

	// Initialize the helm (but do not install tiller).
	if _, err := exec(fmt.Sprintf("helm --home %s init --client-only", helmRepoDir)); err != nil {
		return "", err
	}

	// TODO cleanup
	// Adding cni dependency as a workaround for now.
	// if _, err := exec(fmt.Sprintf("helm --home %s repo add istio.io %s",
	// 	helmRepoDir, "https://storage.googleapis.com/istio-prerelease/daily-build/master-latest-daily/charts")); err != nil {
	// 	return "", err
	// }

	// Package the chart dir.
	if _, err := exec(fmt.Sprintf("helm --home %s package %s -d %s", helmRepoDir, chartDir, chartBuildDir)); err != nil {
		return "", err
	}
	return exec(fmt.Sprintf("helm --home %s template %s --name %s --namespace %s %s %s",
		helmRepoDir, chartDir, deploymentName, namespace, valuesFileString, valuesString))
}

func exec(cmd string) (string, error) {
	scopes.CI.Infof("executing: %s", cmd)
	str, err := shell.Execute(true, cmd)
	if err != nil {
		err = errors.Wrapf(err, "error (%s) executing command: %s", str, cmd)
		scopes.CI.Errorf("%v", err)
	}
	return str, err
}
