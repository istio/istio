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

package ca

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/istio/security/pkg/adapter/kms"
	"istio.io/istio/security/pkg/cmd"
	"istio.io/istio/security/pkg/k8s/controller"
	k8ssecret "istio.io/istio/security/pkg/k8s/secret"
	caerror "istio.io/istio/security/pkg/pki/error"
	"istio.io/istio/security/pkg/pki/util"
	certutil "istio.io/istio/security/pkg/util"
	"istio.io/pkg/log"
	"istio.io/pkg/probe"
)

const (
	// istioCASecretType is the Istio secret annotation type.
	istioCASecretType = "istio.io/ca-root"
	// istioIntermediateCASecretType is the Istio secret annotation type for intermediate CA.
	istioIntermediateCASecretType = "istio.io/intermediate-ca"

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

	// CAEncryptedKeySecret stores the key/cert of self-signed CA for persistency purpose.
	CAEncryptedKeySecret = "istio-ca-encrypted-key"
	// CACertSecret is the name of the secret that stores the CA certificates.
	CACertSecret = "istio-ca-cert"
	// CACertCSRSecret is the name of the secret that store the CA certificate's CSR.
	CACertCSRSecret = "istio-ca-cert-csr"

	// The standard key size to use when generating an RSA private key
	rsaKeySize = 2048

	defaultSecretAccessRetryInterval = time.Millisecond * 500
	defaultSecretAccessTimeout       = time.Second * 2
	defaultCertReadRetryInterval     = time.Second * 2
)

var pkiCaLog = log.RegisterScope("pkica", "Citadel CA log", 0)

// caTypes is the enum for the CA type.
type caTypes int

const (
	// selfSignedCA means the Istio CA uses a self signed certificate.
	selfSignedCA caTypes = iota
	// pluggedCertCA means the Istio CA uses a operator-specified key/cert.
	pluggedCertCA
	// kmsBackedCA means the Istio CA stores the encrypted signing key leveraging a KMS.
	kmsBackedCA
)

// IstioCAOptions holds the configurations for creating an Istio CA.
// TODO(myidpt): remove IstioCAOptions.
type IstioCAOptions struct {
	CAType caTypes

	DefaultCertTTL time.Duration
	MaxCertTTL     time.Duration
	CARSAKeySize   int

	KeyCertBundle util.KeyCertBundle

	LivenessProbeOptions *probe.Options
	ProbeCheckInterval   time.Duration

	// Config for creating self-signed root cert rotator.
	RotatorConfig *SelfSignedCARootCertRotatorConfig
}

// NewSelfSignedIstioCAOptions returns a new IstioCAOptions instance using self-signed certificate.
func NewSelfSignedIstioCAOptions(ctx context.Context,
	rootCertGracePeriodPercentile int, caCertTTL, rootCertCheckInverval, defaultCertTTL,
	maxCertTTL time.Duration, org string, dualUse bool, namespace string,
	readCertRetryInterval time.Duration, client corev1.CoreV1Interface,
	rootCertFile string, enableJitter bool, caRSAKeySize int) (caOpts *IstioCAOptions, err error) {
	// For the first time the CA is up, if readSigningCertOnly is unset,
	// it generates a self-signed key/cert pair and write it to CASecret.
	// For subsequent restart, CA will reads key/cert from CASecret.
	caSecret, scrtErr := client.Secrets(namespace).Get(context.TODO(), CASecret, metav1.GetOptions{})
	if scrtErr != nil && readCertRetryInterval > time.Duration(0) {
		pkiCaLog.Infof("Citadel in signing key/cert read only mode. Wait until secret %s:%s can be loaded...", namespace, CASecret)
		ticker := time.NewTicker(readCertRetryInterval)
		for scrtErr != nil {
			select {
			case <-ticker.C:
				if caSecret, scrtErr = client.Secrets(namespace).Get(context.TODO(), CASecret, metav1.GetOptions{}); scrtErr == nil {
					pkiCaLog.Infof("Citadel successfully loaded the secret.")
				}
			case <-ctx.Done():
				pkiCaLog.Errorf("Secret waiting thread is terminated.")
				return nil, fmt.Errorf("secret waiting thread is terminated")
			}
		}
	}

	caOpts = &IstioCAOptions{
		CAType:         selfSignedCA,
		DefaultCertTTL: defaultCertTTL,
		MaxCertTTL:     maxCertTTL,
		RotatorConfig: &SelfSignedCARootCertRotatorConfig{
			CheckInterval:      rootCertCheckInverval,
			caCertTTL:          caCertTTL,
			retryInterval:      cmd.ReadSigningCertRetryInterval,
			retryMax:           cmd.ReadSigningCertRetryMax,
			certInspector:      certutil.NewCertUtil(rootCertGracePeriodPercentile),
			caStorageNamespace: namespace,
			dualUse:            dualUse,
			org:                org,
			rootCertFile:       rootCertFile,
			enableJitter:       enableJitter,
			client:             client,
		},
	}
	if scrtErr != nil {
		pkiCaLog.Infof("Failed to get secret (error: %s), will create one", scrtErr)

		options := util.CertOptions{
			TTL:          caCertTTL,
			Org:          org,
			IsCA:         true,
			IsSelfSigned: true,
			RSAKeySize:   caRSAKeySize,
			IsDualUse:    dualUse,
		}
		pemCert, pemKey, ckErr := util.GenCertKeyFromOptions(options)
		if ckErr != nil {
			return nil, fmt.Errorf("unable to generate CA cert and key for self-signed CA (%v)", ckErr)
		}

		rootCerts, err := util.AppendRootCerts(pemCert, rootCertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to append root certificates (%v)", err)
		}

		if caOpts.KeyCertBundle, err = util.NewVerifiedKeyCertBundleFromPem(pemCert, pemKey, nil, rootCerts); err != nil {
			return nil, fmt.Errorf("failed to create CA KeyCertBundle (%v)", err)
		}

		// Write the key/cert back to secret so they will be persistent when CA restarts.
		secret := k8ssecret.BuildSecret("", CASecret, namespace, nil, nil, nil, pemCert, pemKey, istioIntermediateCASecretType)
		if _, err = client.Secrets(namespace).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
			pkiCaLog.Errorf("Failed to write secret to CA (error: %s). Abort.", err)
			return nil, fmt.Errorf("failed to create CA due to secret write error")
		}
		pkiCaLog.Infof("Using self-generated public key: %v", string(rootCerts))
	} else {
		pkiCaLog.Infof("Load signing key and cert from existing secret %s:%s", caSecret.Namespace, caSecret.Name)
		rootCerts, err := util.AppendRootCerts(caSecret.Data[caCertID], rootCertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to append root certificates (%v)", err)
		}
		if caOpts.KeyCertBundle, err = util.NewVerifiedKeyCertBundleFromPem(caSecret.Data[caCertID],
			caSecret.Data[caPrivateKeyID], nil, rootCerts); err != nil {
			return nil, fmt.Errorf("failed to create CA KeyCertBundle (%v)", err)
		}
		pkiCaLog.Infof("Using existing public key: %v", string(rootCerts))
	}
	return caOpts, nil
}

// NewPluggedCertIstioCAOptions returns a new IstioCAOptions instance using given certificate.
func NewPluggedCertIstioCAOptions(certChainFile, signingCertFile, signingKeyFile, rootCertFile string,
	defaultCertTTL, maxCertTTL time.Duration, caRSAKeySize int) (caOpts *IstioCAOptions, err error) {
	caOpts = &IstioCAOptions{
		CAType:         pluggedCertCA,
		DefaultCertTTL: defaultCertTTL,
		MaxCertTTL:     maxCertTTL,
		CARSAKeySize:   caRSAKeySize,
	}
	if _, err := os.Stat(signingKeyFile); err != nil {
		// self generating for testing or local, non-k8s run
		options := util.CertOptions{
			TTL:          3650 * 24 * time.Hour, // TODO: pass the flag here as well (or pass MeshConfig )
			Org:          "cluster.local",       // TODO: pass trustDomain ( or better - pass MeshConfig )
			IsCA:         true,
			IsSelfSigned: true,
			RSAKeySize:   caRSAKeySize,
			IsDualUse:    true, // hardcoded to true for K8S as well
		}
		pemCert, pemKey, ckErr := util.GenCertKeyFromOptions(options)
		if ckErr != nil {
			return nil, fmt.Errorf("unable to generate CA cert and key for self-signed CA (%v)", ckErr)
		}

		rootCerts, err := util.AppendRootCerts(pemCert, rootCertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to append root certificates (%v)", err)
		}

		if caOpts.KeyCertBundle, err = util.NewVerifiedKeyCertBundleFromPem(pemCert, pemKey, nil, rootCerts); err != nil {
			return nil, fmt.Errorf("failed to create CA KeyCertBundle (%v)", err)
		}

		return caOpts, nil
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

	return caOpts, nil
}

// NewKMSBackedCAOptions creates a CAOptions for a CA with the KMS encryption.
func NewKMSBackedCAOptions(ctx context.Context,
	defaultCertTTL, maxCertTTL time.Duration, org string, dualUse bool, namespace string,
	client corev1.CoreV1Interface, caRSAKeySize int, kmsEndpoint string, kekID []byte) (
	caOpts *IstioCAOptions, err error) {
	// KMS integration
	// 1. if the signing key secret doesn't exist:
	//      Call the KMS to generate the encrypted key and write it to the secret, and get the decypted key from KMS.
	//    else:
	//      Load the signing key from secret and decrypt it by calling the KMS.
	// 2. if the cert secret doesn't exist:
	//      if the CSR secret doesn't exist:
	//        Generate the CSR from the private key, and write it into the secret
	//      Wait until the cert secret presents
	// 3. Load the cert from the secret, and set it with the signing key into the CA keycertbundle.
	caOpts = &IstioCAOptions{
		CAType:         kmsBackedCA,
		DefaultCertTTL: defaultCertTTL,
		MaxCertTTL:     maxCertTTL,
		CARSAKeySize:   caRSAKeySize,
	}
	kes := kms.KeyEncryptionService{
		Endpoint: kmsEndpoint,
	}

	timeout := time.Hour * 10
	err = kes.Connect(timeout)
	if err != nil {
		pkiCaLog.Errorf("Failed to initialize KMS (error: %s). Abort.", err)
		return nil, fmt.Errorf("failed to create CA due to failure in initializing the KMS")
	}
	defer kes.Cancel()

	srtCtr := controller.NewCaSecretController(client)
	// Read the encrypted CA signing key.
	keySecret, keyReadErr := srtCtr.LoadCASecretWithRetry(
		CAEncryptedKeySecret, namespace, defaultSecretAccessRetryInterval, defaultSecretAccessTimeout)

	// CA signing key secret does not exist, generate one and write it to secret.

	if keyReadErr != nil {
		pkiCaLog.Infof("Failed to read encrypted key from secret (error: %s). We will generate one", keyReadErr)

		encDEK, keyGenErr := kes.GenerateDEK(kekID)
		if keyGenErr != nil {
			pkiCaLog.Errorf("Failed to generate DEK (error: %v). Abort.", keyGenErr)
			return nil, fmt.Errorf("failed to create CA due to failure in generating DEK")
		}

		encSKey, keyGenErr := kes.GenerateSKey(kekID, encDEK, caRSAKeySize, kms.RSA)
		if keyGenErr != nil {
			pkiCaLog.Errorf("Failed to generate encrypted CA key (error: %v). Abort.", keyGenErr)
			return nil, fmt.Errorf("failed to create CA due to failure in generating encrypted CA key")
		}

		// Write the key ID and encrypted key to secret so they will be shared and persistent when CA restarts.
		keySecret = k8ssecret.BuildSecretForEncryptedKey(CAEncryptedKeySecret, namespace, kekID, encDEK,
			encSKey, istioIntermediateCASecretType)

		if keyWriteErr := srtCtr.CreateCASecretWithRetry(keySecret, defaultSecretAccessRetryInterval,
			defaultSecretAccessTimeout); keyWriteErr != nil {
			pkiCaLog.Errorf("Failed to write secret to CA (error: %v). Abort.", keyWriteErr)
			return nil, fmt.Errorf("failed to create CA due to encrypted key secret write error")
		}
		pkiCaLog.Info("Newly generated encrypted signing key is successfully written into secret.")
	}

	kekID = keySecret.Data[k8ssecret.KEKID]
	encDEK := keySecret.Data[k8ssecret.EncryptedDEK]
	encSKey := keySecret.Data[k8ssecret.EncryptedSKey]
	if len(kekID) == 0 || len(encDEK) == 0 || len(encSKey) == 0 {
		pkiCaLog.Errorf("Failed to read KEK ID %s, encrypted DEK %s, and encrypted SKey %s in secret %s.",
			k8ssecret.KEKID, k8ssecret.EncryptedDEK, k8ssecret.EncryptedSKey, CAEncryptedKeySecret)
		return nil, fmt.Errorf("failed to create CA due to malformed encrypted signing key secret")
	}
	sKey, keyErr := kes.GetSKey(kekID, encDEK, encSKey)
	if keyErr != nil {
		pkiCaLog.Errorf("Failed to decrypt CA key (error: %v). Abort.", keyErr)
		return nil, fmt.Errorf("failed to create CA due to failure in decrypting CA key")
	}

	// Read the CA certificate from secret.
	certSecret, certReadErr := srtCtr.LoadCASecretWithRetry(
		CACertSecret, namespace, defaultSecretAccessRetryInterval, defaultSecretAccessTimeout)

	if certReadErr != nil {
		// CA cert certificate does not exist, check if the CSR secret exists. If not, generate one for admin to sign.
		pkiCaLog.Infof("Failed to read CA cert from secret (error: %v). We will check if the CSR exists.", certReadErr)
		if _, csrReadErr := srtCtr.LoadCASecretWithRetry(
			CACertCSRSecret, namespace, defaultSecretAccessRetryInterval, defaultSecretAccessTimeout); csrReadErr != nil {
			// CA cert CSR does not exist, create one.
			pkiCaLog.Infof("Failed to read CA cert CSR from secret (error: %v). We will create one.", certReadErr)
			options := util.CertOptions{
				Org:          org,
				IsCA:         true,
				IsSelfSigned: true,
				IsDualUse:    dualUse,
			}
			csr, csrGenErr := util.GenCSRwithExistingKey(options, sKey)
			if csrGenErr != nil {
				pkiCaLog.Errorf("Failed to generate CSR: %v.", csrGenErr)
				return nil, fmt.Errorf("failed to create CA due to CSR generate failure")
			}

			// Use the organization to identify the CSR and use as the AAD.
			csrID := []byte(org)

			encCSR, keyErr := kes.AuthenticatedEncrypt(kekID, encDEK, csrID, csr)
			if keyErr != nil {
				pkiCaLog.Errorf("Failed to encrypt CSR (error: %v). Abort.", keyErr)
				return nil, fmt.Errorf("failed to encrypt CSR")
			}

			csrSecret := k8ssecret.BuildSecretForCSR(CACertCSRSecret, namespace, kekID, encDEK, csrID, encCSR, istioIntermediateCASecretType)
			if csrWriteErr := srtCtr.CreateCASecretWithRetry(csrSecret, defaultSecretAccessRetryInterval,
				defaultSecretAccessTimeout); csrWriteErr != nil {
				pkiCaLog.Errorf("Failed to write CSR to secret %v.", csrGenErr)
				return nil, fmt.Errorf("failed to create CA due to CSR write failure")
			}
			pkiCaLog.Infof("Newly generated CSR is successfully written into secret %s.", CACertCSRSecret)
		} else {
			pkiCaLog.Infof("CSR exists in secret %s.", CACertCSRSecret)
		}

		// Watch the certificate resource. Blocking.
		pkiCaLog.Infof("Wait until the CA certificate secret %s.%s can be loaded...", namespace, CACertSecret)
		ticker := time.NewTicker(defaultCertReadRetryInterval)
		for certReadErr != nil {
			select {
			case <-ticker.C:
				if certSecret, certReadErr = client.Secrets(namespace).Get(
					context.TODO(), CACertSecret, metav1.GetOptions{}); certReadErr == nil {
					pkiCaLog.Infof("Successfully loaded the secret.")
				}
			case <-ctx.Done():
				pkiCaLog.Errorf("CA certificate secret waiting thread is terminated.")
				return nil, fmt.Errorf("CA certificate secret waiting thread is terminated")
			}
		}
	}

	// Verify cert chain with previously loaded CA cert in HSM
	verified, verifyErr := kes.VerifyCertChain(certSecret.Data[CertChainID])
	if verifyErr != nil {
		pkiCaLog.Errorf("Failed to verify certificate chain (error: %v). Abort.", verifyErr)
		return nil, fmt.Errorf("failed to verify certificate chain")
	} else if !verified {
		pkiCaLog.Errorf("Certificate chain verification failed")
		return nil, fmt.Errorf("certificate chain verification failed")
	}

	if caOpts.KeyCertBundle, err = util.NewVerifiedKeyCertBundleFromPem(nil, sKey, certSecret.Data[CertChainID],
		nil); err != nil {
		return nil, fmt.Errorf("failed to create CA KeyCertBundle (%v)", err)
	}
	pkiCaLog.Info("Loaded decrypted CA key and CA certificate into memory.")
	certBytes, _, certChainBytes, rootBytes := caOpts.KeyCertBundle.GetAllPem()
	pkiCaLog.Infof("CA cert:\n%s\nintermediate certs:\n%s\nroot cert:\n%s", certBytes, certChainBytes, rootBytes)
	return caOpts, nil
}

// IstioCA generates keys and certificates for Istio identities.
type IstioCA struct {
	defaultCertTTL time.Duration
	maxCertTTL     time.Duration
	caRSAKeySize   int

	keyCertBundle util.KeyCertBundle

	livenessProbe *probe.Probe

	// rootCertRotator periodically rotates self-signed root cert for CA. It is nil
	// if CA is not self-signed CA.
	rootCertRotator *SelfSignedCARootCertRotator
}

// NewIstioCA returns a new IstioCA instance.
func NewIstioCA(opts *IstioCAOptions) (*IstioCA, error) {
	ca := &IstioCA{
		maxCertTTL:    opts.MaxCertTTL,
		keyCertBundle: opts.KeyCertBundle,
		livenessProbe: probe.NewProbe(),
		caRSAKeySize:  opts.CARSAKeySize,
	}

	if opts.CAType == selfSignedCA && opts.RotatorConfig.CheckInterval > time.Duration(0) {
		ca.rootCertRotator = NewSelfSignedCARootCertRotator(opts.RotatorConfig, ca)
	}

	// if CA cert becomes invalid before workload cert it's going to cause workload cert to be invalid too,
	// however citatel won't rotate if that happens, this function will prevent that using cert chain TTL as
	// the workload TTL
	defaultCertTTL, err := ca.minTTL(opts.DefaultCertTTL)
	if err != nil {
		return ca, fmt.Errorf("failed to get default cert TTL %s", err.Error())
	}
	ca.defaultCertTTL = defaultCertTTL

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
func (ca *IstioCA) Sign(csrPEM []byte, subjectIDs []string, requestedLifetime time.Duration, forCA bool) (
	[]byte, error) {
	return ca.sign(csrPEM, subjectIDs, requestedLifetime, true, forCA)
}

// SignWithCertChain is similar to Sign but returns the leaf cert and the entire cert chain.
func (ca *IstioCA) SignWithCertChain(csrPEM []byte, subjectIDs []string, requestedLifetime time.Duration, forCA bool) (
	[]byte, error) {
	return ca.signWithCertChain(csrPEM, subjectIDs, requestedLifetime, true, forCA)
}

// GetCAKeyCertBundle returns the KeyCertBundle for the CA.
func (ca *IstioCA) GetCAKeyCertBundle() util.KeyCertBundle {
	return ca.keyCertBundle
}

// GenKeyCert generates a certificate signed by the CA,
// returns the certificate chain and the private key.
func (ca *IstioCA) GenKeyCert(hostnames []string, certTTL time.Duration, checkLifetime bool) ([]byte, []byte, error) {
	opts := util.CertOptions{
		RSAKeySize: rsaKeySize,
	}

	// use the type of private key the CA uses to generate an intermediate CA of that type (e.g. CA cert using RSA will
	// cause intermediate CAs using RSA to be generated)
	_, signingKey, _, _ := ca.keyCertBundle.GetAll()
	if util.IsSupportedECPrivateKey(signingKey) {
		opts.ECSigAlg = util.EcdsaSigAlg
	}

	csrPEM, privPEM, err := util.GenCSR(opts)
	if err != nil {
		return nil, nil, err
	}

	certPEM, err := ca.signWithCertChain(csrPEM, hostnames, certTTL, checkLifetime, false)
	if err != nil {
		return nil, nil, err
	}

	return certPEM, privPEM, nil
}

func (ca *IstioCA) minTTL(defaultCertTTL time.Duration) (time.Duration, error) {
	certChainPem := ca.keyCertBundle.GetCertChainPem()
	if len(certChainPem) == 0 {
		return defaultCertTTL, nil
	}

	certChainExpiration, err := util.TimeBeforeCertExpires(certChainPem, time.Now())
	if err != nil {
		return 0, fmt.Errorf("failed to get cert chain TTL %s", err.Error())
	}

	if certChainExpiration.Seconds() <= 0 {
		return 0, fmt.Errorf("cert chain has expired")
	}

	if defaultCertTTL.Seconds() > certChainExpiration.Seconds() {
		return certChainExpiration, nil
	}

	return defaultCertTTL, nil
}

func (ca *IstioCA) sign(csrPEM []byte, subjectIDs []string, requestedLifetime time.Duration, checkLifetime, forCA bool) ([]byte, error) {
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
		lifetime = ca.defaultCertTTL
	}
	// If checkLifetime is set and the requested TTL is greater than maxCertTTL, return an error
	if checkLifetime && requestedLifetime.Seconds() > ca.maxCertTTL.Seconds() {
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

func (ca *IstioCA) signWithCertChain(csrPEM []byte, subjectIDs []string, requestedLifetime time.Duration, lifetimeCheck,
	forCA bool) ([]byte, error) {
	cert, err := ca.sign(csrPEM, subjectIDs, requestedLifetime, lifetimeCheck, forCA)
	if err != nil {
		return nil, err
	}
	chainPem := ca.GetCAKeyCertBundle().GetCertChainPem()
	if len(chainPem) > 0 {
		cert = append(cert, chainPem...)
	}
	return cert, nil
}
