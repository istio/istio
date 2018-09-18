//  Copyright 2018 HelmInstall Authors
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
	"io/ioutil"
	"os"
	"path"
	"time"

	"istio.io/istio/pkg/test/framework/scopes"
	"istio.io/istio/pkg/test/helm"
	"istio.io/istio/pkg/test/kube"
)

const (
	readyWaitTimeout  = 6 * time.Minute
	namespaceTemplate = `apiVersion: v1
kind: Namespace
metadata:
name: %s
labels:
istio-injection: disabled`
)

// Instance represents an Istio deployment instance that has been performed by this test code.
type Instance struct {
	kubeConfig string

	// The deployment name that is specified when generated the chart.
	deploymentName string

	// Path to the yaml file that is generated from the template.
	yamlFilePath string

	// The deployment namespace
	namespace string
}

// Start a new deployment. Start will return an Instance after the deployment configuration is applied to
// Kubernetes. The callers should wait for the deployment to complete, by calling the Wait() method.
//
// kubeConfig is the kube configuration file to use when calling kubectl.
// chartDir is the directory where the helm charts are located (i.e. ${ISTIO}/install.kubernetes/helm).
// workDir is an output folder for storing intermediate artifacts (i.e. generated yaml etc.)
// hub/tag is the hub & tag values to use, during generation.
// namespace is the target deployment namespace (i.e. "istio-system")
// val is the name of the values file to use when rendering the template. They are located under chartDir)
func Start(kubeConfig, chartDir, workDir, hub, tag, namespace string, val valuesFile) (instance *Instance, err error) {
	scopes.CI.Infof("=== BEGIN: Deploy Istio (via Helm Template) (Chart Dir: %s) ===", chartDir)
	defer func() {
		if err != nil {
			instance = nil
			scopes.CI.Infof("=== FAILED: Deploy Istio ===")
		} else {
			scopes.CI.Infof("=== SUCCEEDED: Deploy Istio ===")
		}
	}()

	instance = &Instance{}

	instance.kubeConfig = kubeConfig
	instance.namespace = namespace

	// Define a deployment name for Helm.
	instance.deploymentName = fmt.Sprintf("%s-%v", namespace, time.Now().UnixNano())
	scopes.CI.Infof("Generated Helm Instance name: %s", instance.deploymentName)

	instance.yamlFilePath = path.Join(workDir, instance.deploymentName+".yaml")

	istioDir := path.Join(chartDir, "istio")
	settings := helm.DefaultSettings()

	settings.Tag = tag
	settings.Hub = hub
	settings.EnableCoreDump = true

	valuesFile := path.Join(istioDir, string(val))

	var generatedYaml string
	if generatedYaml, err = helm.Template(
		instance.deploymentName,
		namespace,
		istioDir,
		valuesFile,
		settings); err != nil {
		scopes.CI.Errorf("Helm chart generation failed: %v", err)
		return
	}

	namespaceData := fmt.Sprintf(namespaceTemplate, namespace)

	generatedYaml = namespaceData + "\n---\n" + generatedYaml

	if err = ioutil.WriteFile(instance.yamlFilePath, []byte(generatedYaml), os.ModePerm); err != nil {
		scopes.CI.Infof("Writing out Helm generated Yaml file failed: %v", err)
		return
	}

	scopes.CI.Infof("Applying Helm generated Yaml file: %s", instance.yamlFilePath)
	if err = kube.Apply(kubeConfig, namespace, instance.yamlFilePath); err != nil {
		scopes.CI.Errorf("Instance of Helm generated Yaml file failed: %v", err)
	}

	return
}

// Wait for installation to complete.
func (i *Instance) Wait(a *kube.Accessor) error {
	scopes.CI.Infof("=== BEGIN: Wait for Istio deployment to quiesce ===")
	if err := a.WaitUntilPodsInNamespaceAreReady(i.namespace, readyWaitTimeout); err != nil {
		scopes.CI.Errorf("Wait for Istio pods failed: %v", err)
		scopes.CI.Infof("=== FAILED: Wait for Istio deployment to quiesce ===")
		return err
	}
	scopes.CI.Infof("=== SUCCEEDED: Wait for Istio deployment to quiesce ===")

	return nil
}

// Delete this deployment instance.
func (i *Instance) Delete() error {
	return kube.Delete(i.kubeConfig, i.yamlFilePath)
}
