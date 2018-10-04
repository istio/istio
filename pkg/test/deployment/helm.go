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
	"io/ioutil"
	"os"
	"path"
	"strconv"
	"time"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/scopes"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/shell"
)

// generate a map[string]string for easy processing.
func (s *Settings) generateHelmValues() map[string]string {
	// TODO: Add more flags, as needed.
	return map[string]string{
		"global.tag":                                 s.Images.Tag,
		"global.hub":                                 s.Images.Hub,
		"global.imagePullPolicy":                     string(s.Images.ImagePullPolicy),
		"global.proxy.enableCoreDump":                boolString(s.EnableCoreDump),
		"global.mtls.enabled":                        boolString(s.GlobalMtlsEnabled),
		"galley.enabled":                             boolString(s.GalleyEnabled),
		"gateways.istio-ingressgateway.replicaCount": uintString(s.IngressGateway.ReplicaCount),
		"gateways.istio-ingressgateway.autoscaleMin": uintString(s.IngressGateway.AutoscaleMin),
		"gateways.istio-ingressgateway.autoscaleMax": uintString(s.IngressGateway.AutoscaleMax),
	}
}

func newHelmDeployment(s *Settings, a *kube.Accessor, chartDir string, valuesFile valuesFile) (*Instance, error) {
	instance := &Instance{}

	instance.kubeConfig = s.KubeConfig
	instance.namespace = s.Namespace

	// Define a deployment name for Helm.
	deploymentName := fmt.Sprintf("%s-%v", s.Namespace, time.Now().UnixNano())
	scopes.CI.Infof("Generated Helm Instance name: %s", deploymentName)

	instance.yamlFilePath = path.Join(s.WorkDir, deploymentName+".yaml")

	vFile := path.Join(chartDir, string(valuesFile))

	var err error
	var generatedYaml string
	if generatedYaml, err = HelmTemplate(
		deploymentName,
		s.Namespace,
		env.IstioChartDir,
		vFile,
		s); err != nil {
		return nil, fmt.Errorf("chart generation failed: %v", err)
	}

	// TODO: This is Istio deployment specific. We may need to remove/reconcile this as a parameter
	// when we support Helm deployment of non-Istio artifacts.
	namespaceData := fmt.Sprintf(namespaceTemplate, s.Namespace)

	generatedYaml = namespaceData + generatedYaml

	if err = ioutil.WriteFile(instance.yamlFilePath, []byte(generatedYaml), os.ModePerm); err != nil {
		return nil, fmt.Errorf("unable to write helm generated yaml: %v", err)
	}

	scopes.CI.Infof("Applying Helm generated Yaml file: %s", instance.yamlFilePath)
	if err = kube.Apply(s.KubeConfig, s.Namespace, instance.yamlFilePath); err != nil {
		return nil, fmt.Errorf("kube apply of generated yaml filed: %v", err)
	}

	if err = instance.wait(s.Namespace, a); err != nil {
		return nil, err
	}

	return instance, nil
}

// HelmTemplate calls "helm template".
func HelmTemplate(deploymentName, namespace, chartDir, valuesFile string, s *Settings) (string, error) {
	valuesString := ""
	if s != nil {
		for k, v := range s.generateHelmValues() {
			valuesString += fmt.Sprintf(" --set %s=%s", k, v)
		}
	}

	valuesFileString := ""
	if valuesFile != "" {
		valuesFileString = fmt.Sprintf(" --values %s", valuesFile)
	}

	str, err := shell.Execute(
		"helm template %s --name %s --namespace %s%s%s",
		chartDir, deploymentName, namespace, valuesFileString, valuesString)
	if err == nil {
		return str, nil
	}

	return "", fmt.Errorf("%v: %s", err, str)
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func uintString(v uint) string {
	return strconv.FormatUint(uint64(v), 10)
}
