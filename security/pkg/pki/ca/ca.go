// Copyright 2017 Istio Authors
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

package ca

import (
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/probe"
	"istio.io/istio/security/pkg/pki/util"
)

const (
	// istioCASecretType is the Istio secret annotation type.
	istioCASecretType = "istio.io/ca-root"

	// cACertChainID is the CA certificate chain file.
	cACertID = "ca-cert.pem"
	// cAPrivateKeyID is the private key file of CA.
	cAPrivateKeyID = "ca-key.pem"
	// cASecret stores the key/cert of self-signed CA for persistency purpose.
	cASecret = "istio-ca-secret"

	// The size of a private key for a self-signed Istio CA.
	caKeySize = 2048
)

// CertificateAuthority contains methods to be supported by a CA.
type CertificateAuthority interface {
	// Sign generates a certificate for a workload or CA, from the given CSR and TTL.
	Sign(csrPEM []byte, ttl time.Duration, forCA bool) ([]byte, error)
	// GetRootCertificate retrieves the root certificate from CA.
	GetRootCertificate() []byte
}

// IstioCAOptions holds the configurations for creating an Istio CA.
type IstioCAOptions struct {
	CertChainBytes   []byte
	CertTTL          time.Duration
	MaxCertTTL       time.Duration
	SigningCertBytes []byte
	SigningKeyBytes  []byte
	RootCertBytes    []byte

	LivenessProbeOptions *probe.Options
}

// IstioCA generates keys and certificates for Istio identities.
type IstioCA struct {
	certTTL     time.Duration
	maxCertTTL  time.Duration
	signingCert *x509.Certificate
	signingKey  crypto.PrivateKey

	certChainBytes []byte
	rootCertBytes  []byte
	livenessProbe  *probe.Probe
}

// NewSelfSignedIstioCAOptions returns a new IstioCAOptions instance using self-signed certificate.
func NewSelfSignedIstioCAOptions(caCertTTL, certTTL, maxCertTTL time.Duration, org string, namespace string,
	core corev1.SecretsGetter) (*IstioCAOptions, error) {

	// For the first time the CA is up, it generates a self-signed key/cert pair and write it to
	// cASecret. For subsequent restart, CA will reads key/cert from cASecret.
	caSecret, err := core.Secrets(namespace).Get(cASecret, metav1.GetOptions{})
	opts := &IstioCAOptions{
		CertTTL:    certTTL,
		MaxCertTTL: maxCertTTL,
	}
	if err != nil {
		log.Infof("Failed to get secret (error: %s), will create one", err)

		options := util.CertOptions{
			TTL:          caCertTTL,
			Org:          org,
			IsCA:         true,
			IsSelfSigned: true,
			RSAKeySize:   caKeySize,
		}
		pemCert, pemKey, err := util.GenCertKeyFromOptions(options)
		if err != nil {
			return nil, fmt.Errorf("unable to generate CA cert and key for self-signed CA (%v)", err)
		}

		opts.SigningCertBytes = pemCert
		opts.SigningKeyBytes = pemKey
		opts.RootCertBytes = pemCert

		// Rewrite the key/cert back to secret so they will be persistent when CA restarts.
		secret := &apiv1.Secret{
			Data: map[string][]byte{
				cACertID:       pemCert,
				cAPrivateKeyID: pemKey,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      cASecret,
				Namespace: namespace,
			},
			Type: istioCASecretType,
		}
		_, err = core.Secrets(namespace).Create(secret)
		if err != nil {
			log.Errorf("Failed to write secret to CA (error: %s). This CA will not persist when restart.", err)
		}
	} else {
		// Reuse existing key/cert in secrets.
		// TODO(wattli): better handle the logic when the key/cert are invalid.
		opts.SigningCertBytes = caSecret.Data[cACertID]
		opts.SigningKeyBytes = caSecret.Data[cAPrivateKeyID]
		opts.RootCertBytes = caSecret.Data[cACertID]
	}

	return opts, nil
}

// NewIstioCA returns a new IstioCA instance.
func NewIstioCA(opts *IstioCAOptions) (*IstioCA, error) {
	ca := &IstioCA{
		certTTL:    opts.CertTTL,
		maxCertTTL: opts.MaxCertTTL,

		livenessProbe: probe.NewProbe(),
	}

	ca.certChainBytes = copyBytes(opts.CertChainBytes)
	ca.rootCertBytes = copyBytes(opts.RootCertBytes)

	var err error
	ca.signingCert, err = util.ParsePemEncodedCertificate(opts.SigningCertBytes)
	if err != nil {
		return nil, err
	}

	ca.signingKey, err = util.ParsePemEncodedKey(opts.SigningKeyBytes)
	if err != nil {
		return nil, err
	}

	if err := ca.verify(); err != nil {
		return nil, err
	}

	if opts.LivenessProbeOptions.IsValid() {
		livenessProbeController := probe.NewFileController(opts.LivenessProbeOptions)
		ca.livenessProbe.RegisterProbe(livenessProbeController, "liveness")
		livenessProbeController.Start()
		ca.livenessProbe.SetAvailable(nil)
	}

	return ca, nil
}

// GetRootCertificate returns the PEM-encoded root certificate.
func (ca *IstioCA) GetRootCertificate() []byte {
	return copyBytes(ca.rootCertBytes)
}

// Sign takes a PEM-encoded certificate signing request and returns a signed
// certificate.
func (ca *IstioCA) Sign(csrPEM []byte, ttl time.Duration, forCA bool) ([]byte, error) {
	csr, err := util.ParsePemEncodedCSR(csrPEM)
	if err != nil {
		return nil, err
	}

	// If the requested TTL is greater than maxCertTTL, apply maxCertTTL as the TTL.
	if ttl.Seconds() > ca.maxCertTTL.Seconds() {
		return nil, fmt.Errorf(
			"requested TTL %s is greater than the max allowed TTL %s", ttl, ca.maxCertTTL)
	}

	certBytes, err := util.GenCertFromCSR(csr, ca.signingCert, csr.PublicKey, ca.signingKey, ttl, forCA)
	if err != nil {
		return nil, err
	}

	block := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	}
	cert := pem.EncodeToMemory(block)

	// Also append intermediate certs into the chain.
	chain := append(cert, ca.certChainBytes...)

	return chain, nil
}

// verify that the cert chain, root cert and signing key/cert match.
func (ca *IstioCA) verify() error {
	// Create another CertPool to hold the root.
	rcp := x509.NewCertPool()
	rcp.AppendCertsFromPEM(ca.rootCertBytes)

	icp := x509.NewCertPool()
	icp.AppendCertsFromPEM(ca.certChainBytes)

	opts := x509.VerifyOptions{
		Intermediates: icp,
		Roots:         rcp,
	}

	chains, err := ca.signingCert.Verify(opts)
	if len(chains) == 0 || err != nil {
		return errors.New(
			"invalid parameters: cannot verify the signing cert with the provided root chain and cert pool")
	}
	return nil
}

func copyBytes(src []byte) []byte {
	bs := make([]byte, len(src))
	copy(bs, src)
	return bs
}
