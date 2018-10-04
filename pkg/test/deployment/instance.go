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

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/framework/scopes"
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

// Instance represents an Istio deployment instance that has been performed by this test code.
type Instance struct {
	kubeConfig string

	// The deployment namespace.
	namespace string

	// Path to the yaml file that is generated from the template.
	yamlFilePath string
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
