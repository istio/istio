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

// Package signer implements a CA signer that uses keys stored on local disk.
package signer

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	capi "k8s.io/api/certificates/v1"

	"istio.io/istio/pkg/test/csrctrl/authority"
	"istio.io/istio/security/pkg/pki/util"
)

type Signer struct {
	caProvider *caProvider
	CertTTL    time.Duration
}

func NewSigner(signerRoot, signerName string, certificateDuration time.Duration) (*Signer, error) {
	caProvider, err := newCAProvider(signerRoot, signerName)
	if err != nil {
		return nil, err
	}

	ret := &Signer{
		caProvider: caProvider,
		CertTTL:    certificateDuration,
	}
	return ret, nil
}

func (s *Signer) Sign(x509cr *x509.CertificateRequest, usages []capi.KeyUsage, requestedLifetime time.Duration, appendRootCert bool) ([]byte, error) {
	currCA, err := s.caProvider.currentCA()
	if err != nil {
		return nil, err
	}
	der, err := currCA.Sign(x509cr.Raw, authority.PermissiveSigningPolicy{
		TTL:    requestedLifetime,
		Usages: usages,
	})
	if err != nil {
		return nil, err
	}

	_, err = x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("error decoding DER certificate bytes: %s", err.Error())
	}

	pemBytes := bytes.NewBuffer([]byte{})
	err = pem.Encode(pemBytes, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err != nil {
		return nil, fmt.Errorf("error encoding certificate PEM: %s", err.Error())
	}

	intermediateCerts, err := util.AppendRootCerts(pemBytes.Bytes(), s.caProvider.caIntermediate.CertFile)
	if err != nil {
		return nil, fmt.Errorf("failed to append intermediate certificates (%v)", err)
	}
	if appendRootCert {
		rootCerts, err := util.AppendRootCerts(intermediateCerts, s.caProvider.caLoader.CertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to append root certificates (%v)", err)
		}
		return rootCerts, nil
	}
	return intermediateCerts, nil
}

func (s *Signer) GetRootCerts() string {
	return s.caProvider.caLoader.CertFile
}
