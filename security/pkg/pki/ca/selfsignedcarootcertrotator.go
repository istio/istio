// Copyright 2019 Istio Authors
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
	"encoding/base64"
	"math/rand"
	"time"

	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/k8s/configmap"
	"istio.io/istio/security/pkg/k8s/controller"
	"istio.io/istio/security/pkg/pki/util"
	certutil "istio.io/istio/security/pkg/util"

	v1 "k8s.io/api/core/v1"
)

var rootCertRotatorLog = log.RegisterScope("rootCertRotator", "Self-signed CA root cert rotator log", 0)

type SelfSignedCARootCertRotatorConfig struct {
	certInspector      certutil.CertUtil
	caStorageNamespace string
	org                string
	client             corev1.CoreV1Interface
	CheckInterval      time.Duration
	caCertTTL          time.Duration
	retryInterval      time.Duration
	dualUse            bool
	enableJitter       bool
}

// SelfSignedCARootCertRotator automatically checks self-signed signing root
// certificate and rotates root certificate if it is going to expire.
type SelfSignedCARootCertRotator struct {
	configMapController *configmap.Controller
	caSecretController  *controller.CaSecretController
	config              *SelfSignedCARootCertRotatorConfig
	backOffTime         time.Duration
	ca                  *IstioCA
}

// NewSelfSignedCARootCertRotator returns a new root cert rotator instance that
// rotates self-signed root cert periodically.
func NewSelfSignedCARootCertRotator(config *SelfSignedCARootCertRotatorConfig,
	ca *IstioCA) *SelfSignedCARootCertRotator {
	rotator := &SelfSignedCARootCertRotator{
		configMapController: configmap.NewController(config.caStorageNamespace, config.client),
		caSecretController:  controller.NewCaSecretController(config.client),
		config:              config,
		ca:                  ca,
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
func (rotator *SelfSignedCARootCertRotator) Run(rootCertRotatorChan chan struct{}) {
	if rotator.config.enableJitter {
		rootCertRotatorLog.Infof("Jitter is enabled, wait %s before "+
			"starting root cert rotator.", rotator.backOffTime.String())
		time.Sleep(rotator.backOffTime)
	}
	ticker := time.NewTicker(rotator.config.CheckInterval)
	for {
		select {
		case <-ticker.C:
			rootCertRotatorLog.Info("Check and rotate root cert.")
			rotator.checkAndRotateRootCert()
		case _, ok := <-rootCertRotatorChan:
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
		rotator.config.caStorageNamespace, rotator.config.retryInterval, 30*time.Second)

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
	caSecret *v1.Secret) {
	if caSecret == nil {
		rootCertRotatorLog.Errorf("root cert secret %s is nil, skip cert rotation job",
			CASecret)
		return
	}
	// Check root certificate expiration time in CA secret
	waitTime, err := rotator.config.certInspector.GetWaitTime(caSecret.Data[caCertID], time.Now(), time.Duration(0))
	if err == nil && waitTime > 0 {
		rootCertRotatorLog.Info("Root cert is not about to expire, skipping root cert rotation.")
		caCertInMem, _, _, _ := rotator.ca.GetCAKeyCertBundle().GetAllPem()
		// If CA certificate is different from the CA certificate in local key
		// cert bundle, it implies that other Citadels have updated istio-ca-secret.
		// Reload root certificate into key cert bundle.
		if !bytes.Equal(caCertInMem, caSecret.Data[caCertID]) {
			rootCertRotatorLog.Warn("CA cert in KeyCertBundle does not match CA cert in " +
				"istio-ca-secret. Start to reload root cert into KeyCertBundle")
			if err := rotator.ca.GetCAKeyCertBundle().VerifyAndSetAll(caSecret.Data[caCertID],
				caSecret.Data[caPrivateKeyID], nil, caSecret.Data[caCertID]); err != nil {
				rootCertRotatorLog.Errorf("failed to reload root cert into KeyCertBundle (%v)", err)
			}
			rootCertRotatorLog.Info("Successfully reloaded root cert into KeyCertBundle.")
		}
		return
	}

	rootCertRotatorLog.Infof("Refresh root certificate, root cert is about to expire: %s", err.Error())
	options := util.CertOptions{
		TTL:           rotator.config.caCertTTL,
		SignerPrivPem: caSecret.Data[caPrivateKeyID],
		Org:           rotator.config.org,
		IsCA:          true,
		IsSelfSigned:  true,
		RSAKeySize:    caKeySize,
		IsDualUse:     rotator.config.dualUse,
	}
	pemCert, pemKey, ckErr := util.GenRootCertFromExistingKey(options)
	if ckErr != nil {
		rootCertRotatorLog.Errorf("unable to generate CA cert and key for self-signed CA: %s", ckErr.Error())
		return
	}

	caSecret.Data[caCertID] = pemCert
	caSecret.Data[caPrivateKeyID] = pemKey
	if err = rotator.caSecretController.UpdateCASecretWithRetry(caSecret, rotator.config.retryInterval, 30*time.Second); err != nil {
		rootCertRotatorLog.Errorf("Failed to write secret to CA secret (error: %s). "+
			"Abort new root certificate.", err.Error())
		return
	}
	rootCertRotatorLog.Infof("A new self-generated root certificate is written into secret: %v", string(pemCert))

	if err := rotator.ca.GetCAKeyCertBundle().VerifyAndSetAll(pemCert, pemKey, nil, pemCert); err != nil {
		rootCertRotatorLog.Errorf("failed to create CA KeyCertBundle (%v)", err)
		return
	}

	certEncoded := base64.StdEncoding.EncodeToString(rotator.ca.GetCAKeyCertBundle().GetRootCertPem())
	if err = rotator.configMapController.InsertCATLSRootCertWithRetry(
		certEncoded, rotator.config.retryInterval, 30*time.Second); err != nil {
		rootCertRotatorLog.Errorf("Failed to write self-signed Citadel's root cert "+
			"to configmap (%s). Node agents will not be able to connect.",
			err.Error())
	}
	rootCertRotatorLog.Infof("Updated CA KeyCertBundle using existing public key: %v", string(pemCert))
}
