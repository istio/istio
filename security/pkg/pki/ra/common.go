// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package ra

import (
	"bytes"
	"crypto/x509"
	"encoding/asn1"
	"fmt"
	"strings"
	"time"

	clientset "k8s.io/client-go/kubernetes"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/slices"
	raerror "istio.io/istio/security/pkg/pki/error"
	"istio.io/istio/security/pkg/pki/util"
	caserver "istio.io/istio/security/pkg/server/ca"
)

// RegistrationAuthority : Registration Authority interface.
type RegistrationAuthority interface {
	caserver.CertificateAuthority
	// SetCACertificatesFromMeshConfig sets the CACertificates using the ones from mesh config
	SetCACertificatesFromMeshConfig([]*meshconfig.MeshConfig_CertificateData)
	// GetRootCertFromMeshConfig returns the root cert for the specific signer in mesh config
	GetRootCertFromMeshConfig(signerName string) ([]byte, error)
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
	K8sClient clientset.Interface
	// TrustDomain
	TrustDomain string
	// CertSignerDomain info
	CertSignerDomain string
}

const (
	// ExtCAK8s : Integrate with external CA using k8s CSR API
	ExtCAK8s CaExternalType = "ISTIOD_RA_KUBERNETES_API"

	// DefaultExtCACertDir : Location of external CA certificate
	DefaultExtCACertDir string = "./etc/external-ca-cert"
)

// ValidateCSR : Validate all SAN extensions in csrPEM match authenticated identities and
// verify additional CSR fields.
func ValidateCSR(csrPEM []byte, subjectIDs []string) bool {
	csr, err := util.ParsePemEncodedCSR(csrPEM)
	if err != nil {
		return false
	}
	if err := csr.CheckSignature(); err != nil {
		return false
	}
	csrIDs, err := util.ExtractIDs(csr.Extensions)
	if err != nil {
		return false
	}
	for _, s1 := range csrIDs {
		if !slices.Contains(subjectIDs, s1) {
			return false
		}
	}

	// Verify Istio CSR was created by Istio-Proxy by generating template using same the
	// same process as performed on the client and comparing the generated CSR with the
	// received CSR.

	// Convert csrIDs to Host comma separated string
	hosts := strings.Join(csrIDs, ",")
	// genCSRTemplate is the expected CSR template. The template's extensions are marshaled
	// in the ExtraExtensions field. CreateCertificateRequest would normally add the SAN extensions
	// from ExtraExtensions to the Extensions field. However, we are only generating the
	// template here and not the actual CSR since we do not know the exact signing mechanisms
	// of the client. Additionally, if we compared extensions the the generated CSR and the original
	// CSR would match since the hosts where constructed from the extracted SAN extensions.
	genCSRTemplate, err := util.GenCSRTemplate(util.CertOptions{Host: hosts})
	if err != nil {
		return false
	}
	// ztunnel CSRs do not have a Subject, fill in an empty "O=" to be consistent with `GenCSRTemplate`'s behavior
	if len(csr.Subject.Organization) == 0 {
		csr.Subject.Organization = []string{""}
	}
	if !compareCSRs(csr, genCSRTemplate) {
		return false
	}
	return true
}

// Compare Workload CSRs
// Verification of the CSR fields based on expected setting from Istio CSR creation in
// security/pkg/pki/util/generate_csr.go and security/pi/nodeagent/cache/secretcache.go
func compareCSRs(orgCSR, genCSR *x509.CertificateRequest) bool {
	// Compare the CSR fields
	if orgCSR == nil || genCSR == nil {
		return false
	}

	orgSubj, err := asn1.Marshal(orgCSR.Subject.ToRDNSequence())
	if err != nil {
		return false
	}
	gensubj, err := asn1.Marshal(genCSR.Subject.ToRDNSequence())
	if err != nil {
		return false
	}

	if !bytes.Equal(orgSubj, gensubj) {
		return false
	}
	// Expected length is 0 or 1
	if len(orgCSR.URIs) > 1 {
		return false
	}
	// Expected length is 0
	if len(orgCSR.EmailAddresses) != len(genCSR.EmailAddresses) {
		return false
	}
	// Expected length is 0
	if len(orgCSR.IPAddresses) != len(genCSR.IPAddresses) {
		return false
	}
	// Expected length is 0
	if len(orgCSR.DNSNames) != len(genCSR.DNSNames) {
		return false
	}
	// Only SAN extensions are expected in the orgCSR
	for _, extension := range orgCSR.Extensions {
		switch {
		case extension.Id.Equal(util.OidSubjectAlternativeName):
		default:
			return false // no other extension used.
		}
	}
	// ExtraExtensions should not be populated in the orgCSR
	return len(orgCSR.ExtraExtensions) == 0
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

// preSign : Validation checks to execute before signing certificates
func preSign(raOpts *IstioRAOptions, csrPEM []byte, subjectIDs []string, requestedLifetime time.Duration, forCA bool) (time.Duration, error) {
	if forCA {
		return requestedLifetime, raerror.NewError(raerror.CSRError,
			fmt.Errorf("unable to generate CA certificates"))
	}
	if !ValidateCSR(csrPEM, subjectIDs) {
		return requestedLifetime, raerror.NewError(raerror.CSRError, fmt.Errorf(
			"unable to validate SAN Identities in CSR"))
	}
	// If the requested requestedLifetime is non-positive, apply the default TTL.
	lifetime := requestedLifetime
	if requestedLifetime.Seconds() <= 0 {
		lifetime = raOpts.DefaultCertTTL
	}
	// If the requested TTL is greater than maxCertTTL, return an error
	if requestedLifetime.Seconds() > raOpts.MaxCertTTL.Seconds() {
		return lifetime, raerror.NewError(raerror.TTLError, fmt.Errorf(
			"requested TTL %s is greater than the max allowed TTL %s", requestedLifetime, raOpts.MaxCertTTL))
	}
	return lifetime, nil
}
