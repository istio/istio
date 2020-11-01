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

	cert "k8s.io/api/certificates/v1beta1"
	certclient "k8s.io/client-go/kubernetes/typed/certificates/v1beta1"

	"istio.io/istio/security/pkg/k8s/chiron"
	caerror "istio.io/istio/security/pkg/pki/error"
	"istio.io/istio/security/pkg/pki/util"
)

// IstioRA : Main type definition of IstioRA
type IstioRA struct {
	keyCertBundle util.KeyCertBundle
	raOpts        *IstioRAOptions
	csrInterface  certclient.CertificatesV1beta1Interface
	// TODO: Create a watcher for the rootCA file
}

// IstioRAOptions : Configuration Options for the IstioRA
type IstioRAOptions struct {
	DefaultCertTTL time.Duration
	MaxCertTTL     time.Duration
	caCertFile     string
	caSigner       string
}

// NewK8sRAOptions : Initialize Kubernates Options
func NewK8sRAOptions(defaultCertTTL, maxCertTTL time.Duration,
	caCertFile, caSigner string) *IstioRAOptions {
	raOptions := &IstioRAOptions{
		DefaultCertTTL: defaultCertTTL,
		MaxCertTTL:     maxCertTTL,
		caCertFile:     caCertFile,
		caSigner:       caSigner}
	return raOptions
}

// NewK8sRA : Create a RA that interfaces with K8S CSR CA
func NewK8sRA(raOpts *IstioRAOptions, csrInterface certclient.CertificatesV1beta1Interface) (*IstioRA, error) {
	keyCertBundle, err := util.NewKeyCertBundleWithRootCertFromFile(raOpts.caCertFile)
	if err != nil {
		return nil, caerror.NewError(caerror.CSRError, fmt.Errorf("error creating Certificate Bundle for k8s CA"))
	}
	istioRA := &IstioRA{csrInterface: csrInterface,
		raOpts:        raOpts,
		keyCertBundle: keyCertBundle}
	return istioRA, nil
}

// Validate all SAN extensions in csrPEM match authenticated identities
func (ra *IstioRA) validateCSR(csrPEM []byte, subjectIDs []string) bool {
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

func (ra *IstioRA) k8sSign(k8sCsrInterface certclient.CertificateSigningRequestInterface,
	csrPEM []byte, csrName string, caCertFile string) ([]byte, error) {
	csrSpec := &cert.CertificateSigningRequestSpec{
		SignerName: &ra.raOpts.caSigner,
		Request:    csrPEM,
		Groups:     []string{"system:authenticated"},
		Usages: []cert.KeyUsage{
			cert.UsageDigitalSignature,
			cert.UsageKeyEncipherment,
			cert.UsageServerAuth,
			cert.UsageClientAuth,
		},
	}
	certChain, _, err := chiron.SignCSRK8s(k8sCsrInterface, csrName, csrSpec, "", caCertFile, false)
	if err != nil {
		return nil, caerror.NewError(caerror.CertGenError, err)
	}
	return certChain, err
}

// Sign takes a PEM-encoded CSR, subject IDs and lifetime, and returns a certificate signed by k8s CA.
func (ra *IstioRA) Sign(csrPEM []byte, subjectIDs []string, requestedLifetime time.Duration, forCA bool) ([]byte, error) {

	if forCA {
		return nil, caerror.NewError(caerror.CSRError, fmt.Errorf(
			"unable to create certifificate for CA"))
	}

	if !ra.validateCSR(csrPEM, subjectIDs) {
		return nil, caerror.NewError(caerror.CSRError, fmt.Errorf(
			"unable to validate SAN Identities in CSR"))
	}

	// TODO: Need to pass the lifetime into the CSR.
	/*	If the requested requestedLifetime is non-positive, apply the default TTL.
			lifetime := requestedLifetime
			if requestedLifetime.Seconds() <= 0 {
				lifetime = ra.defaultCertTTL
		}
	*/

	// If the requested TTL is greater than maxCertTTL, return an error
	if requestedLifetime.Seconds() > ra.raOpts.MaxCertTTL.Seconds() {
		return nil, caerror.NewError(caerror.TTLError, fmt.Errorf(
			"requested TTL %s is greater than the max allowed TTL %s", requestedLifetime, ra.raOpts.MaxCertTTL))
	}
	csrName := chiron.GenCsrName()
	return ra.k8sSign(ra.csrInterface.CertificateSigningRequests(), csrPEM, csrName, ra.raOpts.caCertFile)
}

// SignWithCertChain is similar to Sign but returns the leaf cert and the entire cert chain.
func (ra *IstioRA) SignWithCertChain(csrPEM []byte, subjectIDs []string, ttl time.Duration, forCA bool) ([]byte, error) {
	cert, err := ra.Sign(csrPEM, subjectIDs, ttl, forCA)
	if err != nil {
		return nil, err
	}
	chainPem := ra.GetCAKeyCertBundle().GetCertChainPem()
	if len(chainPem) > 0 {
		cert = append(cert, chainPem...)
	}
	return cert, nil
}

// GetCAKeyCertBundle returns the KeyCertBundle for the CA.
func (ra *IstioRA) GetCAKeyCertBundle() util.KeyCertBundle {
	return ra.keyCertBundle
}
