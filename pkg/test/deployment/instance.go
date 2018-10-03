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

	kubeCore "k8s.io/api/core/v1"

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

	// Hub/Tag/ImagePullPolicy docker image settings to be used during generation.
	Hub             string
	Tag             string
	ImagePullPolicy kubeCore.PullPolicy

	// Namespace is the target deployment namespace (i.e. "istio-system").
	Namespace string
}

// Instance represents an Istio deployment instance that has been performed by this test code.
type Instance struct {
	kubeConfig string

	// The deployment namespace.
	namespace string

	// Path to the yaml file that is generated from the template.
	yamlFilePath string
}

func newHelmDeployment(s *Settings, a *kube.Accessor, chartDir string, valuesFile valuesFile) (*Instance, error) {
	instance := &Instance{}

	instance.kubeConfig = s.KubeConfig
	instance.namespace = s.Namespace

	// Define a deployment name for Helm.
	deploymentName := fmt.Sprintf("%s-%v", s.Namespace, time.Now().UnixNano())
	scopes.CI.Infof("Generated Helm Instance name: %s", deploymentName)

	instance.yamlFilePath = path.Join(s.WorkDir, deploymentName+".yaml")

	settings := helm.DefaultSettings()

	settings.Tag = s.Tag
	settings.Hub = s.Hub
	settings.ImagePullPolicy = s.ImagePullPolicy
	settings.EnableCoreDump = true

	vFile := path.Join(chartDir, string(valuesFile))

	var err error
	var generatedYaml string
	if generatedYaml, err = helm.Template(
		deploymentName,
		s.Namespace,
		env.IstioChartDir,
		vFile,
		settings); err != nil {
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

func newYamlDeployment(s *Settings, a *kube.Accessor, yamlFile string) (*Instance, error) {
	instance := &Instance{}

	instance.kubeConfig = s.KubeConfig
	instance.namespace = s.Namespace
	instance.yamlFilePath = yamlFile

	scopes.CI.Infof("Applying Yaml file: %s", instance.yamlFilePath)
	if err := kube.Apply(s.KubeConfig, s.Namespace, instance.yamlFilePath); err != nil {
		return nil, fmt.Errorf("kube apply of generated yaml filed: %v", err)
	}

	if err := instance.wait(s.Namespace, a); err != nil {
		return nil, err
	}

	return instance, nil
}

// Wait for installation to complete.
func (i *Instance) wait(namespace string, a *kube.Accessor) error {
	if err := a.WaitUntilPodsInNamespaceAreReady(namespace); err != nil {
		scopes.CI.Errorf("Wait for Istio pods failed: %v", err)
		return err
	}

	return nil
}

// Delete this deployment instance.
func (i *Instance) Delete(a *kube.Accessor, wait bool) (err error) {

	if err = kube.Delete(i.kubeConfig, i.namespace, i.yamlFilePath); err != nil {
		scopes.CI.Warnf("Error deleting deployment: %v", err)
	}

	if wait {
		// TODO: Just for waiting for deployment namespace deletion may not be enough. There are CRDs
		// and roles/rolebindings in other parts of the system as well. We should also wait for deletion of them.
		if e := a.WaitForNamespaceDeletion(i.namespace); e != nil {
			scopes.CI.Warnf("Error waiting for environment deletion: %v", e)
			err = multierror.Append(err, e)
		}
	}

	return
}
