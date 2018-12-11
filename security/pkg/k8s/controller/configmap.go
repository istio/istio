// Copyright 2018 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	istioSecurityConfigMapName = "istio-security"
	caTLSRootCertName          = "caTLSRootCert"
)

type ConfigMapController struct {
	core      corev1.CoreV1Interface
	namespace string
}

// NewConfigMapController creates a new ConfigMapController.
func NewConfigMapController(namespace string, core corev1.CoreV1Interface) *ConfigMapController {
	return &ConfigMapController{
		namespace: namespace,
		core:      core,
	}
}

// InsertCATLSRootCert updates the CA TLS root certificate in the configmap.
func (c ConfigMapController) InsertCATLSRootCert(value string) error {
	configmap, err := c.core.ConfigMaps(c.namespace).Get(istioSecurityConfigMapName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("Configmap %s write error %v", istioSecurityConfigMapName, err)
	}
	configmap.Data[caTLSRootCertName] = value
	if _, err = c.core.ConfigMaps(c.namespace).Update(configmap); err != nil {
		return fmt.Errorf("Configmap %s write error %v", istioSecurityConfigMapName, err)
	}
	return nil
}

// GetCATLSRootCert gets the CA TLS root certificate from the configmap.
func (c ConfigMapController) GetCATLSRootCert() (string, error) {
	configmap, err := c.core.ConfigMaps(c.namespace).Get(istioSecurityConfigMapName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("Configmap %s write error %v", istioSecurityConfigMapName, err)
	}
	rootCert := configmap.Data[caTLSRootCertName]
	if rootCert == "" {
		return "", fmt.Errorf("Failed to retrieve the CA TLS root certificate from configmap %s:%s",
			istioSecurityConfigMapName, caTLSRootCertName)
	}

	return rootCert, nil
}
