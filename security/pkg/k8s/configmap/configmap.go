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

package configmap

import (
	"context"
	"fmt"
	"time"

	"istio.io/pkg/log"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

var configMapLog = log.RegisterScope("configMapController", "ConfigMap controller log", 0)

const (
	IstioSecurityConfigMapName = "istio-security"
	CATLSRootCertName          = "caTLSRootCert"
)

// Controller manages the CA TLS root cert in ConfigMap.
type Controller struct {
	core      corev1.CoreV1Interface
	namespace string
}

// NewController creates a new Controller.
func NewController(namespace string, core corev1.CoreV1Interface) *Controller {
	return &Controller{
		namespace: namespace,
		core:      core,
	}
}

// InsertCATLSRootCert updates the CA TLS root certificate in the configmap.
func (c *Controller) InsertCATLSRootCert(value string) error {
	configmap, err := c.core.ConfigMaps(c.namespace).Get(IstioSecurityConfigMapName, metav1.GetOptions{})
	exists := true
	if err != nil {
		if errors.IsNotFound(err) {
			// Create a new ConfigMap.
			configmap = &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      IstioSecurityConfigMapName,
					Namespace: c.namespace,
				},
				Data: map[string]string{},
			}
			exists = false
		} else {
			return fmt.Errorf("failed to insert CA TLS root cert: %v", err)
		}
	}
	configmap.Data[CATLSRootCertName] = value
	if exists {
		if _, err = c.core.ConfigMaps(c.namespace).Update(configmap); err != nil {
			return fmt.Errorf("failed to insert CA TLS root cert: %v", err)
		}
	} else {
		if _, err = c.core.ConfigMaps(c.namespace).Create(configmap); err != nil {
			return fmt.Errorf("failed to insert CA TLS root cert: %v", err)
		}
	}
	return nil
}

// InsertCATLSRootCertWithRetry updates the CA TLS root certificate in the configmap with
// retries until timeout.
func (c *Controller) InsertCATLSRootCertWithRetry(value string, retryInterval,
	timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := c.InsertCATLSRootCert(value)
	ticker := time.NewTicker(retryInterval)
	for err != nil {
		configMapLog.Errorf("Failed to update root cert in config map: %s", err.Error())
		select {
		case <-ticker.C:
			if err = c.InsertCATLSRootCert(value); err == nil {
				break
			}
		case <-ctx.Done():
			configMapLog.Error("Failed to update root cert in config map until timeout.")
			ticker.Stop()
			return err
		}
	}
	return nil
}

// GetCATLSRootCert gets the CA TLS root certificate from the configmap.
func (c *Controller) GetCATLSRootCert() (string, error) {
	configmap, err := c.core.ConfigMaps(c.namespace).Get(IstioSecurityConfigMapName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get CA TLS root cert: %v", err)
	}
	rootCert := configmap.Data[CATLSRootCertName]
	if rootCert == "" {
		return "", fmt.Errorf("failed to get CA TLS root cert from configmap %s:%s",
			IstioSecurityConfigMapName, CATLSRootCertName)
	}

	return rootCert, nil
}
