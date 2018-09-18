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

package deploy

import (
	"encoding/json"
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
	istioSystemNS    = "istio-system"
	readyWaitTimeout = 6 * time.Minute
)

// ValuesFile is the name of the values file to use for deployment.
type ValuesFile string

const (

	// Istio values file
	Istio ValuesFile = "values-istio.yaml"

	// IstioAuth values file
	IstioAuth ValuesFile = "values-istio-auth.yaml"

	// IstioAuthMcp values file
	IstioAuthMcp ValuesFile = "values-istio-auth-mcp.yaml"

	// IstioAuthMulticluster values file
	IstioAuthMulticluster ValuesFile = "values-istio-auth-multicluster.yaml"

	// IstioDemo values file
	IstioDemo ValuesFile = "values-istio-demo.yaml"

	// IstioDemoAuth values file
	IstioDemoAuth ValuesFile = "values-istio-demo-auth.yaml"

	// IstioGateways values file
	IstioGateways ValuesFile = "values-istio-gateways.yaml"

	// IstioMCP values file
	IstioMCP ValuesFile = "values-istio-mcp.yaml"

	// IstioMulticluster values file
	IstioMulticluster ValuesFile = "values-istio-multicluster.yaml"

	// IstioOneNamespace values file
	IstioOneNamespace ValuesFile = "values-istio-one-namespace.yaml"

	// IstioOneNamespaceAuth values file
	IstioOneNamespaceAuth ValuesFile = "values-istio-one-namespace-auth.yaml"
)

// ByHelmTemplate deploys Istio using the Yaml generated from the Helm Template, with the supplied values file.
func ByHelmTemplate(kubeConfig, chartDir, workDir, hub, tag string, accessor *kube.Accessor, f ValuesFile) (err error) {
	scopes.CI.Infof("=== BEGIN: Install Istio (ByHelmTemplate) (Chart Dir: %s) ===", chartDir)
	defer func() {
		if err != nil {
			scopes.CI.Infof("=== FAILED: Install Istio ===")
		} else {
			scopes.CI.Infof("=== SUCCEEDED: Install Istio ===")
		}
	}()

	// TODO: Support deploying to custom namespaces.
	namespace := istioSystemNS

	// Define a deployment name for Helm.
	deploymentName := fmt.Sprintf("%s-%v", namespace, time.Now().UnixNano())
	scopes.CI.Infof("Generated Helm Deployment name: %s", deploymentName)

	// Create a temporary file to store generated Yaml for Istio deployment.
	var tmpFile *os.File
	if tmpFile, err = ioutil.TempFile(workDir, fmt.Sprintf("istio_yaml_%s", string(f))); err != nil {
		scopes.Framework.Errorf("Temp file generation failed: %v", err)
		return
	}

	istioDir := path.Join(chartDir, "istio")
	settings := defaultIstioSettings()

	settings.Tag = tag
	settings.Hub = hub
	settings.EnableCoreDump = true

	var generatedYaml string
	if generatedYaml, err = helm.Template(
		deploymentName, namespace, istioDir, path.Join(istioDir, string(f)), settings.generateHelmSettings()); err != nil {
		scopes.CI.Errorf("Helm chart generation failed: %v", err)
		return
	}

	// TODO: The namespace file hardwires istio-system.
	namespaceFile := path.Join(path.Dir(chartDir), "namespace.yaml")
	if err = kube.Apply(kubeConfig, "", namespaceFile); err != nil {
		scopes.CI.Errorf("Deployment of namespace file failed: %v", err)
		return
	}

	if err = ioutil.WriteFile(tmpFile.Name(), []byte(generatedYaml), os.ModePerm); err != nil {
		scopes.CI.Infof("Writing out Helm generated Yaml file failed: %v", err)
	}

	scopes.CI.Infof("Applying Helm generated Yaml file: %s", tmpFile.Name())
	if err = kube.Apply(kubeConfig, namespace, tmpFile.Name()); err != nil {
		scopes.CI.Errorf("Deployment of Helm generated Yaml file failed: %v", err)
		return
	}

	if err = accessor.WaitUntilPodsInNamespaceAreReady(namespace, readyWaitTimeout); err != nil {
		scopes.CI.Errorf("Wait for Istio pods failed: %v", err)

		// TODO: This should trigger based on a flag, to avoid delays in the local use-case.
		dumpPodState(kubeConfig, namespace)
		copyPodLogs(kubeConfig, workDir, namespace, accessor)
	}

	return
}

func dumpPodState(kubeConfig, namespace string) {
	if s, err := kube.GetPods(kubeConfig, namespace); err != nil {
		scopes.CI.Errorf("Error getting pods list via kubectl: %v", err)
		// continue on
	} else {
		scopes.CI.Infof("Pods (from Kubectl):\n%s", s)
	}
}

func copyPodLogs(kubeConfig, workDir, namespace string, accessor *kube.Accessor) {
	pods, err := accessor.GetPods(namespace)

	if err != nil {
		scopes.CI.Errorf("Error getting pods for error dump: %v", err)
		return
	}

	for i, pod := range pods {
		scopes.CI.Infof("[Pod %d] %s: phase:%q", i, pod.Name, pod.Status.Phase)
		for j, cs := range pod.Status.ContainerStatuses {
			scopes.CI.Infof("[Container %d/%d] %s: ready:%v", i, j, cs.Name, cs.Ready)
		}

		by, err := json.MarshalIndent(pod, "", "  ")
		if err != nil {
			scopes.CI.Errorf("Error marshaling pod status: %v", err)
		}
		scopes.CI.Infof("Pod Detail: \n%s\n", string(by))
	}

	for _, pod := range pods {
		for _, cs := range pod.Status.ContainerStatuses {
			logs, err := kube.Logs(kubeConfig, namespace, pod.Name, cs.Name)
			if err != nil {
				scopes.CI.Errorf("Error getting logs from pods: %v", err)
				continue
			}

			outFile, err := ioutil.TempFile(workDir, fmt.Sprintf("log_pod_%s_%s", pod.Name, cs.Name))
			if err != nil {
				scopes.CI.Errorf("Error creating temporary file for storing pod log: %v", err)
				continue
			}

			if err := ioutil.WriteFile(outFile.Name(), []byte(logs), os.ModePerm); err != nil {
				scopes.CI.Errorf("Error writing out pod lod to file: %v", err)
			}
		}
	}
}
