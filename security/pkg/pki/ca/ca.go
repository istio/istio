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
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/probe"
	"istio.io/istio/security/pkg/k8s/configmap"
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
	// ReadSigningCertCheckInterval specifies the time to wait between retries on reading the signing key and cert.
	ReadSigningCertCheckInterval = time.Second * 5

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
	// TODO(myidpt): simplify this interface and pass a struct with cert field values instead.
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

// Append root certificates in rootCertFile to the input certificate.
func appendRootCerts(pemCert []byte, rootCertFile string) ([]byte, error) {
	var rootCerts []byte
	if len(pemCert) > 0 {
		// Copy the input certificate
		rootCerts = make([]byte, len(pemCert))
		copy(rootCerts, pemCert)
	}
	if len(rootCertFile) > 0 {
		log.Debugf("append root certificates from %v", rootCertFile)
		certBytes, err := ioutil.ReadFile(rootCertFile)
		if err != nil {
			return rootCerts, fmt.Errorf("failed to read root certificates (%v)", err)
		}
		log.Debugf("The root certificates to be appended is: %v", rootCertFile)
		if len(rootCerts) > 0 {
			// Append a newline after the last cert
			rootCerts = []byte(strings.TrimSuffix(string(rootCerts), "\n") + "\n")
		}
		rootCerts = append(rootCerts, certBytes...)
	}
	return rootCerts, nil
}

// NewSelfSignedIstioCAOptions returns a new IstioCAOptions instance using self-signed certificate.
func NewSelfSignedIstioCAOptions(ctx context.Context, caCertTTL, certTTL, maxCertTTL time.Duration, org string, dualUse bool,
	namespace string, readCertRetryInterval time.Duration, client corev1.CoreV1Interface, rootCertFile string) (caOpts *IstioCAOptions, err error) {
	// For the first time the CA is up, if readSigningCertOnly is unset,
	// it generates a self-signed key/cert pair and write it to CASecret.
	// For subsequent restart, CA will reads key/cert from CASecret.
	caSecret, scrtErr := client.Secrets(namespace).Get(CASecret, metav1.GetOptions{})
	if scrtErr != nil && readCertRetryInterval > 0 {
		log.Infof("Citadel in signing key/cert read only mode. Wait until secret %s:%s can be loaded...", namespace, CASecret)
		ticker := time.NewTicker(readCertRetryInterval)
		for scrtErr != nil {
			select {
			case <-ticker.C:
				if caSecret, scrtErr = client.Secrets(namespace).Get(CASecret, metav1.GetOptions{}); scrtErr == nil {
					log.Infof("Citadel successfully loaded the secret.")
					break
				}
			case <-ctx.Done():
				log.Errorf("Secret waiting thread is terminated.")
				return nil, fmt.Errorf("secret waiting thread is terminated")
			}
		}
	}

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

		rootCerts, err := appendRootCerts(pemCert, rootCertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to append root certificates (%v)", err)
		}

		if caOpts.KeyCertBundle, err = util.NewVerifiedKeyCertBundleFromPem(pemCert, pemKey, nil, rootCerts); err != nil {
			return nil, fmt.Errorf("failed to create CA KeyCertBundle (%v)", err)
		}

		// Rewrite the key/cert back to secret so they will be persistent when CA restarts.
		secret := BuildSecret("", CASecret, namespace, nil, nil, nil, pemCert, pemKey, istioCASecretType)
		if _, err = client.Secrets(namespace).Create(secret); err != nil {
			log.Errorf("Failed to write secret to CA (error: %s). This CA will not persist when restart.", err)
		}
	} else {
		log.Infof("Load signing key and cert from existing secret %s:%s", caSecret.Namespace, caSecret.Name)
		rootCerts, err := appendRootCerts(caSecret.Data[caCertID], rootCertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to append root certificates (%v)", err)
		}
		if caOpts.KeyCertBundle, err = util.NewVerifiedKeyCertBundleFromPem(caSecret.Data[caCertID],
			caSecret.Data[caPrivateKeyID], nil, rootCerts); err != nil {
			return nil, fmt.Errorf("failed to create CA KeyCertBundle (%v)", err)
		}
	}

	if err = updateCertInConfigmap(namespace, client, caOpts.KeyCertBundle.GetRootCertPem()); err != nil {
		log.Errorf("Failed to write Citadel cert to configmap (%v). Node agents will not be able to connect.", err)
	}
	return caOpts, nil
}

// NewPluggedCertIstioCAOptions returns a new IstioCAOptions instance using given certificate.
func NewPluggedCertIstioCAOptions(certChainFile, signingCertFile, signingKeyFile, rootCertFile string,
	certTTL, maxCertTTL time.Duration, namespace string, client corev1.CoreV1Interface) (caOpts *IstioCAOptions, err error) {
	caOpts = &IstioCAOptions{
		CAType:     pluggedCertCA,
		CertTTL:    certTTL,
		MaxCertTTL: maxCertTTL,
	}
	if caOpts.KeyCertBundle, err = util.NewVerifiedKeyCertBundleFromFile(
		signingCertFile, signingKeyFile, certChainFile, rootCertFile); err != nil {
		return nil, fmt.Errorf("failed to create CA KeyCertBundle (%v)", err)
	}

	// Validate that the passed in signing cert can be used as CA.
	// The check can't be done inside `KeyCertBundle`, since bundle could also be used to
	// validate workload certificates (i.e., where the leaf certificate is not a CA).
	b, err := ioutil.ReadFile(signingCertFile)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, fmt.Errorf("invalid PEM encoded certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse X.509 certificate")
	}
	if !cert.IsCA {
		return nil, fmt.Errorf("certificate is not authorized to sign other certificates")
	}

	crt := caOpts.KeyCertBundle.GetCertChainPem()
	if len(crt) == 0 {
		crt = caOpts.KeyCertBundle.GetRootCertPem()
	}
	if err = updateCertInConfigmap(namespace, client, crt); err != nil {
		log.Errorf("Failed to write Citadel cert to configmap (%v). Node agents will not be able to connect.", err)
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

func updateCertInConfigmap(namespace string, client corev1.CoreV1Interface, cert []byte) error {
	certEncoded := base64.StdEncoding.EncodeToString(cert)
	cmc := configmap.NewController(namespace, client)
	return cmc.InsertCATLSRootCert(certEncoded)
}
