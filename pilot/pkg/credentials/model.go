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

package credentials

import (
	"istio.io/istio/pkg/cluster"
)

const (
	// The ID/name for the certificate chain in kubernetes generic secret.
	GenericScrtCert = "cert"
	// The ID/name for the private key in kubernetes generic secret.
	GenericScrtKey = "key"
	// The ID/name for the CA certificate in kubernetes generic secret.
	GenericScrtCaCert = "cacert"
	// The ID/name for the CRL in kubernetes generic secret.
	GenericScrtCRL = "crl"

	// The ID/name for the certificate chain in kubernetes tls secret.
	TLSSecretCert = "tls.crt"
	// The ID/name for the k8sKey in kubernetes tls secret.
	TLSSecretKey = "tls.key"
	// The ID/name for the certificate OCSP staple in kubernetes tls secret
	TLSSecretOcspStaple = "tls.ocsp-staple"
	// The ID/name for the CA certificate in kubernetes tls secret
	TLSSecretCaCert = "ca.crt"
	// The ID/name for the CRL in kubernetes tls secret.
	TLSSecretCrl = "ca.crl"

	// The ID/name for Istio_DataSourceInlineBytes in kubernetes opaque secret.
	DataSourceInlineBytes = "Istio_DataSourceInlineBytes"
)

// CertInfo wraps a certificate, key, and oscp staple information.
type CertInfo struct {
	// The certificate chain
	Cert []byte
	// The private key
	Key []byte
	// The oscp staple
	Staple []byte
	// Certificate Revocation List information
	CRL []byte
}

type Controller interface {
	GetCertInfo(name, namespace string) (certInfo *CertInfo, err error)
	GetCaCert(name, namespace string) (certInfo *CertInfo, err error)
	GetDataSourceKeyAndValue(name, namespace string) (key []byte, cert []byte, err error)
	GetDockerCredential(name, namespace string) (cred []byte, err error)
	Authorize(serviceAccount, namespace string) error
	AddEventHandler(func(name, namespace string))
}

type MulticlusterController interface {
	ForCluster(cluster cluster.ID) (Controller, error)
	AddSecretHandler(func(name, namespace string))
}
