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
	// CASecret stores the key/cert of self-signed CA for persistency purpose.
	CASecret = "istio-ca-secret"
	// CertChainID is the ID/name for the certificate chain file.
	CertChainID = "cert-chain.pem"
	// PrivateKeyID is the ID/name for the private key file.
	PrivateKeyID = "key.pem"
	// RootCertID is the ID/name for the CA root certificate file.
	RootCertID = "root-cert.pem"
	// KeyIDName is the ID/name for the Key ID file.
	KEKID = "kek-id"
	// EncryptedDEK stores the encrypted DEK.
	EncryptedDEK = "encrypted-dek"
	// EncryptedSKey stores the encrypted CA signing key.
	EncryptedSKey = "encrypted-skey"
	// EncryptedCSR is the CSR encrypted with the DEK.
	EncryptedCSR = "encrypted-csr"
	// CSRID is an id of the CSR.  This is used to visually verify CSR before decrypting and signing.
	CSRID = "csr-id"
	// ServiceAccountNameAnnotationKey is the key to specify corresponding service account in the annotation of K8s secrets.
	ServiceAccountNameAnnotationKey = "istio.io/service-account.name"
)

// BuildSecret returns a secret struct, contents of which are filled with parameters passed in.
func BuildSecret(saName, scrtName, namespace string, certChain, privateKey, rootCert, caCert, caPrivateKey []byte, secretType v1.SecretType) *v1.Secret {
	var ServiceAccountNameAnnotation map[string]string
	if saName == "" {
		ServiceAccountNameAnnotation = nil
	} else {
		ServiceAccountNameAnnotation = map[string]string{ServiceAccountNameAnnotationKey: saName}
	}
	return &v1.Secret{
		Data: map[string][]byte{
			CertChainID:    certChain,
			PrivateKeyID:   privateKey,
			RootCertID:     rootCert,
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

// BuildSecretForEncryptedKey returns a secret struct, that stores the KEK ID and the encrypted DEK and SKey.
func BuildSecretForEncryptedKey(scrtName, namespace string, kekID, encDEK, encSKey []byte, secretType v1.SecretType) *v1.Secret {
	return &v1.Secret{
		Data: map[string][]byte{
			KEKID:         kekID,
			EncryptedDEK:  encDEK,
			EncryptedSKey: encSKey,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      scrtName,
			Namespace: namespace,
		},
		Type: secretType,
	}
}

// BuildSecretForCSR returns a secret struct, that stores the certificate signing request.
func BuildSecretForCSR(scrtName, namespace string, kekID, encDEK, csrID, encCSR []byte, secretType v1.SecretType) *v1.Secret {
	return &v1.Secret{
		Data: map[string][]byte{
			KEKID:        kekID,
			EncryptedDEK: encDEK,
			CSRID:        csrID,
			EncryptedCSR: encCSR,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      scrtName,
			Namespace: namespace,
		},
		Type: secretType,
	}
}
