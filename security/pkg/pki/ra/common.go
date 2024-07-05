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
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
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

// The OID for the BasicConstraints extension (See
// https://www.alvestrand.no/objectid/2.5.29.19.html).
var oidBasicConstraints = asn1.ObjectIdentifier{2, 5, 29, 19}

// ValidateCSR : Validate all SAN extensions in csrPEM match authenticated identities,
// validate the signature of the CSR, and verify isCA is false.
func ValidateCSR(csrPEM []byte, subjectIDs []string) bool {
	csr, err := util.ParsePemEncodedCSR(csrPEM)
	if err != nil {
		return false
	}
	if err := csr.CheckSignature(); err != nil {
		return false
	}
	if isCA, err := checkIsCAExtension(csr.Extensions); err != nil || isCA {
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
	return true
}

// basicConstraints is a structure that represents the ASN.1 encoding of the
// BasicConstraints extension.
// The structure is borrowed from
// https://github.com/golang/go/blob/master/src/crypto/x509/x509.go#L975
type basicConstraints struct {
	IsCA       bool `asn1:"optional"`
	MaxPathLen int  `asn1:"optional,default:-1"`
}

func checkIsCAExtension(exts []pkix.Extension) (bool, error) {
	basicExt := extractBasicConstraintsExtension(exts)
	if basicExt == nil {
		pkiRaLog.Debug("BasicConstraints extension is not populated in CSR. Assuming isCA is false.")
		return false, nil
	}
	isCA, err := extractIsCAFromBasicConstraints(basicExt)
	if err != nil {
		return false, fmt.Errorf("failed to extract CA value from BasicConstraints extension (error %v)", err)
	}
	return isCA, nil
}

// extractBasicConstraintsExtension extracts the "basic" extension from
// the given PKIX extension set.
func extractBasicConstraintsExtension(exts []pkix.Extension) *pkix.Extension {
	for _, ext := range exts {
		if ext.Id.Equal(oidBasicConstraints) {
			// We don't need to examine other extensions anymore since a certificate
			// must not include more than one instance of a particular extension. See
			// https://tools.ietf.org/html/rfc5280#section-4.2.
			return &ext
		}
	}
	return nil
}

// extractIsCAFromBasicConstraints takes a BasicConstraints extension and extracts the isCA value.
// The logic is mostly borrowed from
// https://github.com/golang/go/blob/master/src/crypto/x509/x509.go.
func extractIsCAFromBasicConstraints(basicExt *pkix.Extension) (bool, error) {
	if !basicExt.Id.Equal(oidBasicConstraints) {
		return false, fmt.Errorf("the input is not a BasicConstraints extension")
	}
	basicConstraints := basicConstraints{}
	if rest, err := asn1.Unmarshal(basicExt.Value, &basicConstraints); err != nil {
		return false, err
	} else if len(rest) != 0 {
		return false, fmt.Errorf("the BasicConstraints extension is incorrectly encoded")
	}
	return basicConstraints.IsCA, nil
}

// marshalBasicConstraints marshals the isCA value into a BasicConstraints extension.
func marshalBasicConstraints(isCA bool) (*pkix.Extension, error) {
	ext := &pkix.Extension{Id: oidBasicConstraints, Critical: true}
	// Leaving MaxPathLen as zero indicates that no maximum path
	// length is desired, unless MaxPathLenZero is set. A value of
	// -1 causes encoding/asn1 to omit the value as desired.
	var err error
	ext.Value, err = asn1.Marshal(basicConstraints{isCA, -1})
	return ext, err
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
			fmt.Errorf("unable to generate CA certifificates"))
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
