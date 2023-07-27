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

// SecretType contains the secret sub types used by Istio
type SecretType string

const (
	// IstioGenericSecret is a kubernetes generic secret annotated with
	// security.istio.io/genericSecret suggesting its type
	IstioGenericSecret SecretType = "IstioGenericSecret"
	// TLSSecret is a kubernetes generic TLS secret and is the default
	// secret sub type used by Istio
	TLSSecret SecretType = "TLSSecret"
)

// SecretInfo wraps CertInfo and GenericSecretInfo containing information
// about TLSSecret and IstioGenericSecret respectively
type SecretInfo struct {
	Type              SecretType
	CertInfo          *CertInfo
	GenericSecretInfo *GenericSecretInfo
}

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

type GenericSecretInfo struct {
	// The secret key
	Key string
	// The secret value
	Value []byte
}

type Controller interface {
	GetCaCert(name, namespace string) (certInfo *CertInfo, err error)
	GetDockerCredential(name, namespace string) (cred []byte, err error)
	GetSecretInfo(name, namespace string) (secretInfo SecretInfo, err error)
	Authorize(serviceAccount, namespace string) error
	AddEventHandler(func(name, namespace string))
}

type MulticlusterController interface {
	ForCluster(cluster cluster.ID) (Controller, error)
	AddSecretHandler(func(name, namespace string))
}
