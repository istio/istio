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

	"istio.io/istio/pkg/test/framework/scopes"
	"istio.io/istio/pkg/test/kube"
)

// NewYamlDeployment creates a new yaml-based deployment.
func NewYamlDeployment(a *kube.Accessor, kubeConfig, namespace, yamlFile string) (*Instance, error) {
	instance := &Instance{}

	instance.kubeConfig = kubeConfig
	instance.namespace = namespace
	instance.yamlFilePath = yamlFile

	scopes.CI.Infof("Applying Yaml file: %s", instance.yamlFilePath)
	if err := kube.Apply(kubeConfig, namespace, instance.yamlFilePath); err != nil {
		return nil, fmt.Errorf("kube apply of generated yaml filed: %v", err)
	}

	if err := instance.wait(namespace, a); err != nil {
		return nil, err
	}

	return instance, nil
}
