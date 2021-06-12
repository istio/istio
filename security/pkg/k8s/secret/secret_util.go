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

package secret

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// caCertID is the CA certificate chain file.
	caCertID = "ca-cert.pem"
	// caPrivateKeyID is the private key file of CA.
	caPrivateKeyID = "ca-key.pem"
	// certChainID is the ID/name for the certificate chain file.
	certChainID = "cert-chain.pem"
	// privateKeyID is the ID/name for the private key file.
	privateKeyID = "key.pem"
	// rootCertID is the ID/name for the CA root certificate file.
	rootCertID = "root-cert.pem"
	// serviceAccountNameAnnotationKey is the key to specify corresponding service account in the annotation of K8s secrets.
	serviceAccountNameAnnotationKey = "istio.io/service-account.name"
)

// BuildSecret returns a secret struct, contents of which are filled with parameters passed in.
func BuildSecret(saName, scrtName, namespace string, certChain, privateKey, rootCert, caCert, caPrivateKey []byte, secretType v1.SecretType) *v1.Secret {
	var ServiceAccountNameAnnotation map[string]string
	if saName == "" {
		ServiceAccountNameAnnotation = nil
	} else {
		ServiceAccountNameAnnotation = map[string]string{serviceAccountNameAnnotationKey: saName}
	}
	return &v1.Secret{
		Data: map[string][]byte{
			certChainID:    certChain,
			privateKeyID:   privateKey,
			rootCertID:     rootCert,
			caCertID:       caCert,
			caPrivateKeyID: caPrivateKey,
		},
		ObjectMeta: metav1.ObjectMeta{
			Annotations: ServiceAccountNameAnnotation,
			Name:        scrtName,
			Namespace:   namespace,
		},
		Type: secretType,
	}
}
