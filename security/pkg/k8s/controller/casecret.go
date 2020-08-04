// Copyright Istio Authors
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

	"istio.io/pkg/log"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	// The Istio secret annotation type
	IstioSecretType = "istio.io/key-and-cert"

	// The ID/name for the certificate chain file.
	CertChainID = "cert-chain.pem"
	// The ID/name for the private key file.
	PrivateKeyID = "key.pem"
	// The ID/name for the CA root certificate file.
	RootCertID = "root-cert.pem"
)

var k8sControllerLog = log.RegisterScope("secretcontroller", "Citadel kubernetes controller log", 0)

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
	start := time.Now()
	var caSecret *v1.Secret
	var scrtErr error
	for {
		caSecret, scrtErr = csc.client.Secrets(namespace).Get(context.TODO(), secretName, metav1.GetOptions{})
		if scrtErr == nil {
			return caSecret, scrtErr
		}
		k8sControllerLog.Errorf("Failed on loading CA secret %s:%s.",
			namespace, secretName)

		if time.Since(start) > timeout {
			k8sControllerLog.Errorf("Timeout on loading CA secret %s:%s.",
				namespace, secretName)
			return caSecret, scrtErr
		}
		time.Sleep(retryInterval)
	}
}

// UpdateCASecretWithRetry updates CA secret with retries until timeout.
func (csc *CaSecretController) UpdateCASecretWithRetry(caSecret *v1.Secret,
	retryInterval, timeout time.Duration) error {
	start := time.Now()
	for {
		_, scrtErr := csc.client.Secrets(caSecret.Namespace).Update(context.TODO(), caSecret, metav1.UpdateOptions{})
		if scrtErr == nil {
			return nil
		}
		k8sControllerLog.Errorf("Failed on updating CA secret %s:%s.",
			caSecret.Namespace, caSecret.Name)

		if time.Since(start) > timeout {
			k8sControllerLog.Errorf("Timeout on updating CA secret %s:%s.",
				caSecret.Namespace, caSecret.Name)
			return scrtErr
		}
		time.Sleep(retryInterval)
	}
}
