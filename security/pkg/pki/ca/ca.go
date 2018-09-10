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
	"encoding/pem"
	"fmt"
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/probe"
	"istio.io/istio/security/pkg/pki/util"
)

const (
	// istioCASecretType is the Istio secret annotation type.
	istioCASecretType = "istio.io/ca-root"

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
	// ServiceAccountNameAnnotationKey is the key to specify corresponding service account in the annotation of K8s secrets.
	ServiceAccountNameAnnotationKey = "istio.io/service-account.name"

	// The size of a private key for a self-signed Istio CA.
	caKeySize = 2048
)

// caTypes is the enum for the CA type.
type caTypes int

const (
	// selfSignedCA means the Istio CA uses a self signed certificate.
	selfSignedCA caTypes = iota
	// pluggedCertCA means the Istio CA uses a operator-specified key/cert.
	pluggedCertCA
)

// CertificateAuthority contains methods to be supported by a CA.
type CertificateAuthority interface {
	// Sign generates a certificate for a workload or CA, from the given CSR and TTL.
	Sign(csrPEM []byte, subjectIDs []string, ttl time.Duration, forCA bool) ([]byte, error)
	// GetCAKeyCertBundle returns the KeyCertBundle used by CA.
	GetCAKeyCertBundle() util.KeyCertBundle
}

// IstioCAOptions holds the configurations for creating an Istio CA.
// TODO(myidpt): remove IstioCAOptions.
type IstioCAOptions struct {
	CAType caTypes

	CertTTL    time.Duration
	MaxCertTTL time.Duration

	KeyCertBundle util.KeyCertBundle

	LivenessProbeOptions *probe.Options
	ProbeCheckInterval   time.Duration
}

// IstioCA generates keys and certificates for Istio identities.
type IstioCA struct {
	certTTL    time.Duration
	maxCertTTL time.Duration

	keyCertBundle util.KeyCertBundle

	livenessProbe *probe.Probe
}

// NewSelfSignedIstioCAOptions returns a new IstioCAOptions instance using self-signed certificate.
func NewSelfSignedIstioCAOptions(caCertTTL, certTTL, maxCertTTL time.Duration, org string, dualUse bool,
	namespace string, core corev1.SecretsGetter) (caOpts *IstioCAOptions, err error) {
	// For the first time the CA is up, it generates a self-signed key/cert pair and write it to
	// CASecret. For subsequent restart, CA will reads key/cert from CASecret.
	caSecret, scrtErr := core.Secrets(namespace).Get(CASecret, metav1.GetOptions{})
	caOpts = &IstioCAOptions{
		CAType:     selfSignedCA,
		CertTTL:    certTTL,
		MaxCertTTL: maxCertTTL,
	}
	if scrtErr != nil {
		log.Infof("Failed to get secret (error: %s), will create one", scrtErr)

		options := util.CertOptions{
			TTL:          caCertTTL,
			Org:          org,
			IsCA:         true,
			IsSelfSigned: true,
			RSAKeySize:   caKeySize,
			IsDualUse:    dualUse,
		}
		pemCert, pemKey, ckErr := util.GenCertKeyFromOptions(options)
		if ckErr != nil {
			return nil, fmt.Errorf("unable to generate CA cert and key for self-signed CA (%v)", ckErr)
		}

		if caOpts.KeyCertBundle, err = util.NewVerifiedKeyCertBundleFromPem(pemCert, pemKey, nil, pemCert); err != nil {
			return nil, fmt.Errorf("failed to create CA KeyCertBundle (%v)", err)
		}

		// Rewrite the key/cert back to secret so they will be persistent when CA restarts.
		secret := BuildSecret("", CASecret, namespace, nil, nil, nil, pemCert, pemKey, istioCASecretType)
		if _, err = core.Secrets(namespace).Create(secret); err != nil {
			log.Errorf("Failed to write secret to CA (error: %s). This CA will not persist when restart.", err)
		}
	} else {
		if caOpts.KeyCertBundle, err = util.NewVerifiedKeyCertBundleFromPem(caSecret.Data[caCertID],
			caSecret.Data[caPrivateKeyID], nil, caSecret.Data[caCertID]); err != nil {
			return nil, fmt.Errorf("failed to create CA KeyCertBundle (%v)", err)
		}
	}

	return caOpts, nil
}

// NewPluggedCertIstioCAOptions returns a new IstioCAOptions instance using given certificate.
func NewPluggedCertIstioCAOptions(certChainFile, signingCertFile, signingKeyFile, rootCertFile string,
	certTTL, maxCertTTL time.Duration) (caOpts *IstioCAOptions, err error) {
	caOpts = &IstioCAOptions{
		CAType:     pluggedCertCA,
		CertTTL:    certTTL,
		MaxCertTTL: maxCertTTL,
	}
	if caOpts.KeyCertBundle, err = util.NewVerifiedKeyCertBundleFromFile(
		signingCertFile, signingKeyFile, certChainFile, rootCertFile); err != nil {
		return nil, fmt.Errorf("failed to create CA KeyCertBundle (%v)", err)
	}
	return caOpts, nil
}

// NewIstioCA returns a new IstioCA instance.
func NewIstioCA(opts *IstioCAOptions) (*IstioCA, error) {
	ca := &IstioCA{
		certTTL:       opts.CertTTL,
		maxCertTTL:    opts.MaxCertTTL,
		keyCertBundle: opts.KeyCertBundle,
		livenessProbe: probe.NewProbe(),
	}

	return ca, nil
}

// Sign takes a PEM-encoded CSR, subject IDs and lifetime, and returns a signed certificate. If forCA is true,
// the signed certificate is a CA certificate, otherwise, it is a workload certificate.
// TODO(myidpt): Add error code to identify the Sign error types.
func (ca *IstioCA) Sign(csrPEM []byte, subjectIDs []string, requestedLifetime time.Duration, forCA bool) ([]byte, error) {
	signingCert, signingKey, _, _ := ca.keyCertBundle.GetAll()
	if signingCert == nil {
		return nil, NewError(CANotReady, fmt.Errorf("Istio CA is not ready")) // nolint
	}

	csr, err := util.ParsePemEncodedCSR(csrPEM)
	if err != nil {
		return nil, NewError(CSRError, err)
	}

	lifetime := requestedLifetime
	// If the requested requestedLifetime is non-positive, apply the default TTL.
	if requestedLifetime.Seconds() <= 0 {
		lifetime = ca.certTTL
	}
	// If the requested TTL is greater than maxCertTTL, return an error
	if requestedLifetime.Seconds() > ca.maxCertTTL.Seconds() {
		return nil, NewError(TTLError, fmt.Errorf(
			"requested TTL %s is greater than the max allowed TTL %s", requestedLifetime, ca.maxCertTTL))
	}

	certBytes, err := util.GenCertFromCSR(csr, signingCert, csr.PublicKey, *signingKey, subjectIDs, lifetime, forCA)
	if err != nil {
		return nil, NewError(CertGenError, err)
	}

	block := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	}
	cert := pem.EncodeToMemory(block)

	return cert, nil
}

// GetCAKeyCertBundle returns the KeyCertBundle for the CA.
func (ca *IstioCA) GetCAKeyCertBundle() util.KeyCertBundle {
	return ca.keyCertBundle
}

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
