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
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/istio/pkg/probe"
	"istio.io/istio/security/pkg/cmd"
	"istio.io/istio/security/pkg/k8s/configmap"
	k8ssecret "istio.io/istio/security/pkg/k8s/secret"
	caerror "istio.io/istio/security/pkg/pki/error"
	"istio.io/istio/security/pkg/pki/util"
	certutil "istio.io/istio/security/pkg/util"
	"istio.io/pkg/log"
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

// IstioCAOptions holds the configurations for creating an Istio CA.
// TODO(myidpt): remove IstioCAOptions.
type IstioCAOptions struct {
	CAType caTypes

	CertTTL    time.Duration
	MaxCertTTL time.Duration

	KeyCertBundle util.KeyCertBundle

	LivenessProbeOptions *probe.Options
	ProbeCheckInterval   time.Duration

	// Config for creating self-signed root cert rotator.
	RotatorConfig *SelfSignedCARootCertRotatorConfig
}

// NewSelfSignedIstioCAOptions returns a new IstioCAOptions instance using self-signed certificate.
func NewSelfSignedIstioCAOptions(ctx context.Context, readSigningCertOnly bool,
	rootCertGracePeriodPercentile int, caCertTTL, rootCertCheckInverval, certTTL,
	maxCertTTL time.Duration, org string, dualUse bool, namespace string,
	readCertRetryInterval time.Duration, client corev1.CoreV1Interface,
	enableJitter bool) (caOpts *IstioCAOptions, err error) {
	// For the first time the CA is up, if readSigningCertOnly is unset,
	// it generates a self-signed key/cert pair and write it to CASecret.
	// For subsequent restart, CA will reads key/cert from CASecret.
	caSecret, scrtErr := client.Secrets(namespace).Get(CASecret, metav1.GetOptions{})
	if scrtErr != nil && readCertRetryInterval > time.Duration(0) {
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
		RotatorConfig: &SelfSignedCARootCertRotatorConfig{
			CheckInterval:       rootCertCheckInverval,
			caCertTTL:           caCertTTL,
			retryInterval:       cmd.ReadSigningCertRetryInterval,
			certInspector:       certutil.NewCertUtil(rootCertGracePeriodPercentile),
			caStorageNamespace:  namespace,
			dualUse:             dualUse,
			readSigningCertOnly: readSigningCertOnly,
			org:                 org,
			enableJitter:        enableJitter,
			client:              client,
		},
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

		// Write the key/cert back to secret so they will be persistent when CA restarts.
		secret := k8ssecret.BuildSecret("", CASecret, namespace, nil, nil, nil, pemCert, pemKey, istioCASecretType)
		if _, err = client.Secrets(namespace).Create(secret); err != nil {
			log.Errorf("Failed to write secret to CA (error: %s). Abort.", err)
			return nil, fmt.Errorf("failed to create CA due to secret write error")
		}
		log.Infof("Using self-generated public key: %v", string(pemCert))
	} else {
		log.Infof("Load signing key and cert from existing secret %s:%s", caSecret.Namespace, caSecret.Name)
		if caOpts.KeyCertBundle, err = util.NewVerifiedKeyCertBundleFromPem(caSecret.Data[caCertID],
			caSecret.Data[caPrivateKeyID], nil, caSecret.Data[caCertID]); err != nil {
			return nil, fmt.Errorf("failed to create CA KeyCertBundle (%v)", err)
		}
		log.Infof("Using existing public key: %v", string(caSecret.Data[caCertID]))
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
	if err = updateCertInConfigmap(namespace, client, caOpts.KeyCertBundle.GetRootCertPem()); err != nil {
		log.Errorf("Failed to write Citadel cert to configmap (%v). Node agents will not be able to connect.", err)
	}
	return caOpts, nil
}

// IstioCA generates keys and certificates for Istio identities.
type IstioCA struct {
	certTTL    time.Duration
	maxCertTTL time.Duration

	keyCertBundle util.KeyCertBundle

	livenessProbe *probe.Probe

	// rootCertRotator periodically rotates self-signed root cert for CA. It is nil
	// if CA is not self-signed CA.
	rootCertRotator *SelfSignedCARootCertRotator
}

// NewIstioCA returns a new IstioCA instance.
func NewIstioCA(opts *IstioCAOptions) (*IstioCA, error) {
	ca := &IstioCA{
		certTTL:       opts.CertTTL,
		maxCertTTL:    opts.MaxCertTTL,
		keyCertBundle: opts.KeyCertBundle,
		livenessProbe: probe.NewProbe(),
	}

	if opts.CAType == selfSignedCA && opts.RotatorConfig.CheckInterval > time.Duration(0) {
		ca.rootCertRotator = NewSelfSignedCARootCertRotator(opts.RotatorConfig, ca)
	}
	return ca, nil
}

func (ca *IstioCA) Run(stopChan chan struct{}) {
	if ca.rootCertRotator != nil {
		// Start root cert rotator in a separate goroutine.
		go ca.rootCertRotator.Run(stopChan)
	}
}

// Sign takes a PEM-encoded CSR, subject IDs and lifetime, and returns a signed certificate. If forCA is true,
// the signed certificate is a CA certificate, otherwise, it is a workload certificate.
// TODO(myidpt): Add error code to identify the Sign error types.
func (ca *IstioCA) Sign(csrPEM []byte, subjectIDs []string, requestedLifetime time.Duration, forCA bool) ([]byte, error) {
	signingCert, signingKey, _, _ := ca.keyCertBundle.GetAll()
	if signingCert == nil {
		return nil, caerror.NewError(caerror.CANotReady, fmt.Errorf("Istio CA is not ready")) // nolint
	}

	csr, err := util.ParsePemEncodedCSR(csrPEM)
	if err != nil {
		return nil, caerror.NewError(caerror.CSRError, err)
	}

	lifetime := requestedLifetime
	// If the requested requestedLifetime is non-positive, apply the default TTL.
	if requestedLifetime.Seconds() <= 0 {
		lifetime = ca.certTTL
	}
	// If the requested TTL is greater than maxCertTTL, return an error
	if requestedLifetime.Seconds() > ca.maxCertTTL.Seconds() {
		return nil, caerror.NewError(caerror.TTLError, fmt.Errorf(
			"requested TTL %s is greater than the max allowed TTL %s", requestedLifetime, ca.maxCertTTL))
	}

	certBytes, err := util.GenCertFromCSR(csr, signingCert, csr.PublicKey, *signingKey, subjectIDs, lifetime, forCA)
	if err != nil {
		return nil, caerror.NewError(caerror.CertGenError, err)
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

func updateCertInConfigmap(namespace string, client corev1.CoreV1Interface, cert []byte) error {
	certEncoded := base64.StdEncoding.EncodeToString(cert)
	cmc := configmap.NewController(namespace, client)
	return cmc.InsertCATLSRootCert(certEncoded)
}
