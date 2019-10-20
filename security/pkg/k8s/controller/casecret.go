// Copyright 2019 Istio Authors
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
	"context"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/istio/pkg/log"
)

var caSecretControllerLog = log.RegisterScope("caSecretController",
	"Self-signed root cert secret controller log", 0)

// CaSecretController manages the self-signed signing CA secret.
type CaSecretController struct {
	client corev1.CoreV1Interface
}

// NewCaSecretController returns a pointer to a newly constructed SecretController instance.
func NewCaSecretController(core corev1.CoreV1Interface) *CaSecretController {
	cs := &CaSecretController{
		client: core,
	}
	return cs
}

// LoadCASecretWithRetry reads CA secret with retries until timeout.
func (csc *CaSecretController) LoadCASecretWithRetry(secretName, namespace string,
	retryInterval, timeout time.Duration) (*v1.Secret, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	caSecret, scrtErr := csc.client.Secrets(namespace).Get(secretName, metav1.GetOptions{})
	if scrtErr != nil && !errors.IsNotFound(scrtErr) {
		caSecretControllerLog.Errorf("Failed to read secret that holds CA certificate: %s. "+
			"Wait until secret %s:%s can be loaded", scrtErr.Error(), namespace, secretName)
		ticker := time.NewTicker(retryInterval)
		for scrtErr != nil {
			select {
			case <-ticker.C:
				if caSecret, scrtErr = csc.client.Secrets(namespace).Get(secretName, metav1.GetOptions{}); scrtErr == nil {
					break
				}
			case <-ctx.Done():
				caSecretControllerLog.Errorf("Timeout on loading CA secret %s:%s.", namespace, secretName)
				ticker.Stop()
				break
			}
		}
	}
	return caSecret, scrtErr
}

// UpdateCASecretWithRetry updates CA secret with retries until timeout.
func (csc *CaSecretController) UpdateCASecretWithRetry(caSecret *v1.Secret,
	retryInterval, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	_, scrtErr := csc.client.Secrets(caSecret.Namespace).Update(caSecret)
	if scrtErr != nil {
		caSecretControllerLog.Errorf("Failed to update CA secret: %s. Wait until "+
			"secret %s:%s can be updated", scrtErr.Error(), caSecret.Namespace, caSecret.Name)
		ticker := time.NewTicker(retryInterval)
		for scrtErr != nil {
			select {
			case <-ticker.C:
				if caSecret, scrtErr = csc.client.Secrets(caSecret.Namespace).Update(caSecret); scrtErr == nil {
					break
				}
			case <-ctx.Done():
				caSecretControllerLog.Errorf("Timeout on updating CA secret %s:%s.",
					caSecret.Namespace, caSecret.Name)
				ticker.Stop()
				break
			}
		}
	}
	return scrtErr
}
