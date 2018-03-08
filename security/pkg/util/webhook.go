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

package util

import (
	"fmt"
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/pki/ca"
	"istio.io/istio/security/pkg/pki/ca/controller"
	"istio.io/istio/security/pkg/pki/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	keyFile             = "key.pem"
	certFile            = "cert.pem"
	caFile              = "ca.pem"
	secretCreationRetry = 3
	webhookCertValidFor = 24 * 7 * time.Hour
	rsaBits             = 2048
)

// WebhookIdentity stores the information about the webhook service.
type WebhookIdentity struct {
	Service   string
	Secret    string
	Namespace string
	Path      string
}

// GenerateWebhookSecrets generate key and cert for webhook services and stores them in K8S secrets
func GenerateWebhookSecrets(client corev1client.CoreV1Interface, ca ca.CertificateAuthority, webhooks []WebhookIdentity) error {
	caCert, _, _, _ := ca.GetCAKeyCertBundle().GetAllPem()
	for _, w := range webhooks {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      w.Secret,
				Namespace: w.Namespace,
			},
			Type: controller.IstioSecretType,
		}
		_, err := client.Secrets(w.Namespace).Get(w.Secret, metav1.GetOptions{})
		if err != nil {
			csrPEM, keyPEM, err := util.GenCSR(
				util.CertOptions{
					Host:       fmt.Sprintf("%s.%s.svc", w.Service, w.Namespace),
					RSAKeySize: rsaBits,
				})
			if err != nil {
				return err
			}
			certPEM, err := ca.Sign(csrPEM, webhookCertValidFor, false)
			if err != nil {
				return err
			}
			secret.Data = map[string][]byte{
				keyFile:  keyPEM,
				certFile: certPEM,
				caFile:   caCert,
			}
			err = createSecretWithRetry(client.Secrets(w.Namespace), secret)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// Retry several times when create secret to mitigate transient network failures.
func createSecretWithRetry(client corev1client.SecretInterface, secret *corev1.Secret) error {
	var err error
	for i := 0; i < secretCreationRetry; i++ {
		_, err = client.Create(secret)
		if err == nil {
			break
		} else {
			log.Errorf("Failed to create secret in attempt %v/%v, (error: %s)", i+1, secretCreationRetry, err)
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		log.Errorf("Failed to create secret for \"%s\"  (error: %s), retries %v times",
			secret, err, secretCreationRetry)
	}
	return err
}
