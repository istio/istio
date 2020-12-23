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
package ra

import (
	"fmt"
	"time"

	certificatesv1beta1 "k8s.io/client-go/kubernetes/typed/certificates/v1beta1"

	"istio.io/istio/security/pkg/pki/util"
	caserver "istio.io/istio/security/pkg/server/ca"
)

// RegistrationAuthority : Registration Authority interface.
type RegistrationAuthority interface {
	caserver.CertificateAuthority
}

// CaExternalType : Type of External CA integration
type CaExternalType string

// IstioRAOptions : Configuration Options for the IstioRA
type IstioRAOptions struct {
	// ExternalCAType: Integration API type with external CA
	ExternalCAType CaExternalType
	// DefaultCertTTL: Default Certificate TTL
	DefaultCertTTL time.Duration
	// MaxCertTTL: Maximum Certificate TTL that can be requested
	MaxCertTTL time.Duration
	// CaCertFile : File containing PEM encoded CA root certificate of external CA
	CaCertFile string
	// CaSigner : To indicate custom CA Signer name when using external K8s CA
	CaSigner string
	// VerifyAppendCA : Whether to use caCertFile containing CA root cert to verify and append to signed cert-chain
	VerifyAppendCA bool
	// K8sClient : K8s API client
	K8sClient certificatesv1beta1.CertificatesV1beta1Interface
}

const (
	// ExtCAK8s : Integrate with external CA using k8s CSR API
	ExtCAK8s CaExternalType = "ISTIOD_RA_KUBERNETES_API"

	// ExtCAGrpc : Integration with external CA using Istio CA gRPC API
	ExtCAGrpc CaExternalType = "ISTIOD_RA_ISTIO_API"

	// DefaultExtCACertDir : Location of external CA certificate
	DefaultExtCACertDir string = "./etc/external-ca-cert"
)

// ValidateCSR : Validate all SAN extensions in csrPEM match authenticated identities
func ValidateCSR(csrPEM []byte, subjectIDs []string) bool {
	var match bool
	csr, err := util.ParsePemEncodedCSR(csrPEM)
	if err != nil {
		return false
	}
	csrIDs, err := util.ExtractIDs(csr.Extensions)
	if err != nil {
		return false
	}
	for _, s1 := range csrIDs {
		match = false
		for _, s2 := range subjectIDs {
			if s1 == s2 {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}
	return true
}

// NewIstioRA is a factory method that returns an RA that implements the RegistrationAuthority functionality.
// the caOptions defines the external provider
func NewIstioRA(opts *IstioRAOptions) (RegistrationAuthority, error) {
	if opts.ExternalCAType == ExtCAK8s {
		istioRA, err := NewKubernetesRA(opts)
		if err != nil {
			return nil, fmt.Errorf("failed to create an K8s CA: %v", err)
		}
		return istioRA, err
	}
	return nil, fmt.Errorf("invalid CA Name %s", opts.ExternalCAType)
}
