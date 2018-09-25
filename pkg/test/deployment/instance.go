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
	"time"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/env"

	"istio.io/istio/pkg/test/framework/scopes"
	"istio.io/istio/pkg/test/helm"
	"istio.io/istio/pkg/test/kube"
)

const (
	namespaceTemplate = `apiVersion: v1
kind: Namespace
metadata:
  name: %s
  labels:
    istio-injection: disabled
`
)

// Settings for deploying Istio.
type Settings struct {
	// KubeConfig is the kube configuration file to use when calling kubectl.
	KubeConfig string

	// WorkDir is an output folder for storing intermediate artifacts (i.e. generated yaml etc.)
	WorkDir string

	// Hub/Tag is the hub & tag values to use, during generation.
	Hub string
	Tag string

	// Namespace is the target deployment namespace (i.e. "istio-system").
	Namespace string

	// ValuesFile is the name of the values file to use when rendering the template. They are located under
	// repository.IstioChartDir.
	ValuesFile valuesFile
}

// Instance represents an Istio deployment instance that has been performed by this test code.
type Instance struct {
	kubeConfig string

	// The deployment name that is specified when generated the chart.
	deploymentName string

	// The deployment namespace.
	namespace string

	// Path to the yaml file that is generated from the template.
	yamlFilePath string
}

// New deploys Istio. New will start an Istio deployment against Istio, wait for its completion, and return a
// deployment instance to track the lifecycle.
func New(s *Settings, a *kube.Accessor) (instance *Instance, err error) {
	scopes.CI.Info("=== BEGIN: Deploy Istio (via Helm Template) ===")
	defer func() {
		if err != nil {
			instance = nil
			scopes.CI.Infof("=== FAILED: Deploy Istio ===")
		} else {
			scopes.CI.Infof("=== SUCCEEDED: Deploy Istio ===")
		}
	}()

	instance = &Instance{}

	instance.kubeConfig = s.KubeConfig
	instance.namespace = s.Namespace

	// Define a deployment name for Helm.
	instance.deploymentName = fmt.Sprintf("%s-%v", s.Namespace, time.Now().UnixNano())
	scopes.CI.Infof("Generated Helm Instance name: %s", instance.deploymentName)

	instance.yamlFilePath = path.Join(s.WorkDir, instance.deploymentName+".yaml")

	settings := helm.DefaultSettings()

	settings.Tag = s.Tag
	settings.Hub = s.Hub
	settings.EnableCoreDump = true

	valuesFile := path.Join(env.IstioChartDir, string(s.ValuesFile))

	var generatedYaml string
	if generatedYaml, err = helm.Template(
		instance.deploymentName,
		s.Namespace,
		env.IstioChartDir,
		valuesFile,
		settings); err != nil {
		scopes.CI.Errorf("Helm chart generation failed: %v", err)
		return
	}

	namespaceData := fmt.Sprintf(namespaceTemplate, s.Namespace)

	generatedYaml = namespaceData + generatedYaml

	if err = ioutil.WriteFile(instance.yamlFilePath, []byte(generatedYaml), os.ModePerm); err != nil {
		scopes.CI.Infof("Writing out Helm generated Yaml file failed: %v", err)
		return
	}

	scopes.CI.Infof("Applying Helm generated Yaml file: %s", instance.yamlFilePath)
	if err = kube.Apply(s.KubeConfig, s.Namespace, instance.yamlFilePath); err != nil {
		scopes.CI.Errorf("Instance of Helm generated Yaml file failed: %v", err)
		return
	}

	err = instance.wait(s.Namespace, a)

	return
}

// Wait for installation to complete.
func (i *Instance) wait(namespace string, a *kube.Accessor) error {
	scopes.CI.Infof("=== BEGIN: Wait for Istio deployment to quiesce ===")
	if err := a.WaitUntilPodsInNamespaceAreReady(namespace); err != nil {
		scopes.CI.Errorf("Wait for Istio pods failed: %v", err)
		scopes.CI.Infof("=== FAILED: Wait for Istio deployment to quiesce ===")
		return err
	}
	scopes.CI.Infof("=== SUCCEEDED: Wait for Istio deployment to quiesce ===")

	return nil
}

// Delete this deployment instance.
func (i *Instance) Delete(a *kube.Accessor) (err error) {

	if err = kube.Delete(i.kubeConfig, i.yamlFilePath); err != nil {
		scopes.CI.Warnf("Error deleting deployment: %v", err)
	}

	// TODO: Just for waiting for deployment namespace deletion may not be enough. There are CRDs
	// and roles/rolebindings in other parts of the system as well. We should also wait for deletion of them.
	if e := a.WaitForNamespaceDeletion(i.namespace); e != nil {
		scopes.CI.Warnf("Error waiting for environment deletion: %v", e)
		err = multierror.Append(err, e)
	}

	return
}
