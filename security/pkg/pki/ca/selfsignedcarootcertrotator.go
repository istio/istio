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
	"bytes"
	"fmt"
	"math/rand"
	"time"

	v1 "k8s.io/api/core/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/istio/security/pkg/k8s/controller"
	"istio.io/istio/security/pkg/pki/util"
	certutil "istio.io/istio/security/pkg/util"
	"istio.io/pkg/log"
)

var rootCertRotatorLog = log.RegisterScope("rootcertrotator", "Self-signed CA root cert rotator log", 0)

type SelfSignedCARootCertRotatorConfig struct {
	certInspector      certutil.CertUtil
	caStorageNamespace string
	org                string
	rootCertFile       string
	client             corev1.CoreV1Interface
	CheckInterval      time.Duration
	caCertTTL          time.Duration
	retryInterval      time.Duration
	retryMax           time.Duration
	dualUse            bool
	enableJitter       bool
}

// SelfSignedCARootCertRotator automatically checks self-signed signing root
// certificate and rotates root certificate if it is going to expire.
type SelfSignedCARootCertRotator struct {
	caSecretController *controller.CaSecretController
	config             *SelfSignedCARootCertRotatorConfig
	backOffTime        time.Duration
	ca                 *IstioCA
}

// NewSelfSignedCARootCertRotator returns a new root cert rotator instance that
// rotates self-signed root cert periodically.
func NewSelfSignedCARootCertRotator(config *SelfSignedCARootCertRotatorConfig,
	ca *IstioCA,
) *SelfSignedCARootCertRotator {
	rotator := &SelfSignedCARootCertRotator{
		caSecretController: controller.NewCaSecretController(config.client),
		config:             config,
		ca:                 ca,
	}
	if config.enableJitter {
		// Select a back off time in seconds, which is in the range of [0, rotator.config.CheckInterval).
		randSource := rand.NewSource(time.Now().UnixNano())
		randBackOff := rand.New(randSource)
		backOffSeconds := int(time.Duration(randBackOff.Int63n(int64(rotator.config.CheckInterval))).Seconds())
		rotator.backOffTime = time.Duration(backOffSeconds) * time.Second
		rootCertRotatorLog.Infof("Set up back off time %s to start rotator.", rotator.backOffTime.String())
	} else {
		rotator.backOffTime = time.Duration(0)
	}
	return rotator
}

// Run refreshes root certs and updates config map accordingly.
func (rotator *SelfSignedCARootCertRotator) Run(stopCh chan struct{}) {
	if rotator.config.enableJitter {
		rootCertRotatorLog.Infof("Jitter is enabled, wait %s before "+
			"starting root cert rotator.", rotator.backOffTime.String())
		select {
		case <-time.After(rotator.backOffTime):
			rootCertRotatorLog.Infof("Jitter complete, start rotator.")
		case <-stopCh:
			rootCertRotatorLog.Info("Received stop signal, so stop the root cert rotator.")
			return
		}
	}
	ticker := time.NewTicker(rotator.config.CheckInterval)
	for {
		select {
		case <-ticker.C:
			rootCertRotatorLog.Info("Check and rotate root cert.")
			rotator.checkAndRotateRootCert()
		case _, ok := <-stopCh:
			if !ok {
				rootCertRotatorLog.Info("Received stop signal, so stop the root cert rotator.")
				if ticker != nil {
					ticker.Stop()
				}
				return
			}
		}
	}
}

// checkAndRotateRootCert decides whether root cert should be refreshed, and rotates
// root cert for self-signed Citadel.
func (rotator *SelfSignedCARootCertRotator) checkAndRotateRootCert() {
	caSecret, scrtErr := rotator.caSecretController.LoadCASecretWithRetry(CASecret,
		rotator.config.caStorageNamespace, rotator.config.retryInterval, rotator.config.retryMax)

	if scrtErr != nil {
		rootCertRotatorLog.Errorf("Fail to load CA secret %s:%s (error: %s), skip cert rotation job",
			rotator.config.caStorageNamespace, CASecret, scrtErr.Error())
	} else {
		rotator.checkAndRotateRootCertForSigningCertCitadel(caSecret)
	}
}

// checkAndRotateRootCertForSigningCertCitadel checks root cert secret and rotates
// root cert if the current one is about to expire. The rotation uses existing
// root private key to generate a new root cert, and updates root cert secret.
func (rotator *SelfSignedCARootCertRotator) checkAndRotateRootCertForSigningCertCitadel(
	caSecret *v1.Secret,
) {
	if caSecret == nil {
		rootCertRotatorLog.Errorf("root cert secret %s is nil, skip cert rotation job",
			CASecret)
		return
	}
	// Check root certificate expiration time in CA secret
	waitTime, err := rotator.config.certInspector.GetWaitTime(caSecret.Data[CACertFile], time.Now(), time.Duration(0))
	if err == nil && waitTime > 0 {
		rootCertRotatorLog.Info("Root cert is not about to expire, skipping root cert rotation.")
		caCertInMem, _, _, _ := rotator.ca.GetCAKeyCertBundle().GetAllPem()
		// If CA certificate is different from the CA certificate in local key
		// cert bundle, it implies that other Citadels have updated istio-ca-secret.
		// Reload root certificate into key cert bundle.
		if !bytes.Equal(caCertInMem, caSecret.Data[CACertFile]) {
			rootCertRotatorLog.Warn("CA cert in KeyCertBundle does not match CA cert in " +
				"istio-ca-secret. Start to reload root cert into KeyCertBundle")
			rootCerts, err := util.AppendRootCerts(caSecret.Data[CACertFile], rotator.config.rootCertFile)
			if err != nil {
				rootCertRotatorLog.Errorf("failed to append root certificates from file: %s", err.Error())
				return
			}
			if err := rotator.ca.GetCAKeyCertBundle().VerifyAndSetAll(caSecret.Data[CACertFile],
				caSecret.Data[CAPrivateKeyFile], nil, rootCerts); err != nil {
				rootCertRotatorLog.Errorf("failed to reload root cert into KeyCertBundle (%v)", err)
			} else {
				rootCertRotatorLog.Info("Successfully reloaded root cert into KeyCertBundle.")
			}
		}
		return
	}

	rootCertRotatorLog.Infof("Refresh root certificate, root cert is about to expire: %s", err.Error())

	oldCertOptions, err := util.GetCertOptionsFromExistingCert(caSecret.Data[CACertFile])
	if err != nil {
		rootCertRotatorLog.Warnf("Failed to generate cert options from existing root certificate (%v), "+
			"new root certificate may not match old root certificate", err)
	}
	options := util.CertOptions{
		TTL:           rotator.config.caCertTTL,
		SignerPrivPem: caSecret.Data[CAPrivateKeyFile],
		Org:           rotator.config.org,
		IsCA:          true,
		IsSelfSigned:  true,
		RSAKeySize:    rotator.ca.caRSAKeySize,
		IsDualUse:     rotator.config.dualUse,
	}
	// options should be consistent with the one used in NewSelfSignedIstioCAOptions().
	// This is to make sure when rotate the root cert, we don't make unnecessary changes
	// to the certificate or add extra fields to the certificate.
	options = util.MergeCertOptions(options, oldCertOptions)
	pemCert, pemKey, ckErr := util.GenRootCertFromExistingKey(options)
	if ckErr != nil {
		rootCertRotatorLog.Errorf("unable to generate CA cert and key for self-signed CA: %s", ckErr.Error())
		return
	}

	pemRootCerts, err := util.AppendRootCerts(pemCert, rotator.config.rootCertFile)
	if err != nil {
		rootCertRotatorLog.Errorf("failed to append root certificates: %s", err.Error())
		return
	}

	oldCaCert := caSecret.Data[CACertFile]
	oldCaPrivateKey := caSecret.Data[CAPrivateKeyFile]
	oldRootCerts := rotator.ca.GetCAKeyCertBundle().GetRootCertPem()
	if rollback, err := rotator.updateRootCertificate(caSecret, true, pemCert, pemKey, pemRootCerts); err != nil {
		if !rollback {
			rootCertRotatorLog.Errorf("Failed to roll forward root certificate (error: %s). "+
				"Abort new root certificate", err.Error())
			return
		}
		// caSecret is out-of-date. Need to load the latest istio-ca-secret to roll back root certificate.
		_, err = rotator.updateRootCertificate(nil, false, oldCaCert, oldCaPrivateKey, oldRootCerts)
		if err != nil {
			rootCertRotatorLog.Errorf("Failed to roll backward root certificate (error: %s).", err.Error())
		}
		return
	}
	rootCertRotatorLog.Info("Root certificate rotation is completed successfully.")
}

// updateRootCertificate updates root certificate in istio-ca-secret, keycertbundle and configmap. It takes a scrt
// object, cert, and key, and a flag rollForward indicating whether this update is to roll forward root certificate or
// to roll backward.
// updateRootCertificate returns error when any step is failed, and a flag indicating whether a rollback is required.
// Only when rollForward is true and failure happens, the returned rollback flag is true.
func (rotator *SelfSignedCARootCertRotator) updateRootCertificate(caSecret *v1.Secret, rollForward bool, cert, key, rootCert []byte) (bool, error) {
	var err error
	if caSecret == nil {
		caSecret, err = rotator.caSecretController.LoadCASecretWithRetry(CASecret,
			rotator.config.caStorageNamespace, rotator.config.retryInterval, rotator.config.retryMax)
		if err != nil {
			return false, fmt.Errorf("failed to load CA secret %s:%s (error: %s)", rotator.config.caStorageNamespace, CASecret,
				err.Error())
		}
	}
	caSecret.Data[CACertFile] = cert
	caSecret.Data[CAPrivateKeyFile] = key
	if err = rotator.caSecretController.UpdateCASecretWithRetry(caSecret, rotator.config.retryInterval, rotator.config.retryMax); err != nil {
		return false, fmt.Errorf("failed to update CA secret (error: %s)", err.Error())
	}
	rootCertRotatorLog.Infof("Root certificate is written into CA secret: %v", string(cert))
	if err := rotator.ca.GetCAKeyCertBundle().VerifyAndSetAll(cert, key, nil, rootCert); err != nil {
		if rollForward {
			// Rolling forward root certificate fails at keycertbundle update, notify caller to rollback.
			return true, fmt.Errorf("failed to update CA KeyCertBundle (error: %s)", err.Error())
		}
		return false, fmt.Errorf("failed to update CA KeyCertBundle (error: %s)", err.Error())
	}
	rootCertRotatorLog.Infof("Root certificate is updated in CA KeyCertBundle: %v", string(cert))

	return false, nil
}
