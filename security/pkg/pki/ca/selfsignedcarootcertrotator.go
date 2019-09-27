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
	"time"

	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/istio/security/pkg/k8s/configmap"
	"istio.io/istio/security/pkg/k8s/controller"
	"istio.io/istio/security/pkg/monitoring"
	"istio.io/istio/security/pkg/pki/util"
	certutil "istio.io/istio/security/pkg/util"
	"istio.io/pkg/log"

	v1 "k8s.io/api/core/v1"
)

var rootCertRotatorLog = log.RegisterScope("rootCertRotator", "Self-signed CA root cert rotator log", 0)

// SelfSignedCARootCertRotator automatically checks self-signed signing root
// certificate and rotates root certificate if it is going to expire.
type SelfSignedCARootCertRotator struct {
	checkInterval       time.Duration
	caCertTTL           time.Duration
	retryInterval       time.Duration
	certInspector       certutil.CertUtil
	configMapController *configmap.Controller
	caSecretController  *controller.CaSecretController
	caStorageNamespace  string
	dualUse             bool
	readSigningCertOnly bool
	org                 string
	rootCertFile        string
	metrics             monitoring.MonitoringMetrics
	ca                  *IstioCA
}

type SelfSignedCARootCertRotatorConfig struct {
	CheckInterval       time.Duration
	caCertTTL           time.Duration
	retryInterval       time.Duration
	certInspector       certutil.CertUtil
	configMapController *configmap.Controller
	caSecretController  *controller.CaSecretController
	caStorageNamespace  string
	dualUse             bool
	readSigningCertOnly bool
	org                 string
	rootCertFile        string
	metrics             monitoring.MonitoringMetrics
	client              corev1.CoreV1Interface
}

// NewSelfSignedCARootCertRotator returns a new root cert rotator instance that
// rotates self-signed root cert periodically.
func NewSelfSignedCARootCertRotator(config *SelfSignedCARootCertRotatorConfig,
	ca *IstioCA) *SelfSignedCARootCertRotator {
	rotator := &SelfSignedCARootCertRotator{
		checkInterval:       config.CheckInterval,
		caCertTTL:           config.caCertTTL,
		retryInterval:       config.retryInterval,
		certInspector:       config.certInspector,
		configMapController: configmap.NewController(config.caStorageNamespace, config.client),
		caSecretController:  controller.NewCaSecretController(config.client),
		caStorageNamespace:  config.caStorageNamespace,
		dualUse:             config.dualUse,
		readSigningCertOnly: config.readSigningCertOnly,
		org:                 config.org,
		rootCertFile:        config.rootCertFile,
		metrics:             config.metrics,
		ca:                  ca,
	}
	return rotator
}

// Run refreshes root certs and updates config map accordingly.
func (rotator *SelfSignedCARootCertRotator) Run(rootCertRotatorChan chan struct{}) {
	ticker := time.NewTicker(rotator.checkInterval)
	for {
		select {
		case <-ticker.C:
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

type RootUpgradeStatus int

const (
	UpgradeSkip    RootUpgradeStatus = 0
	UpgradeSuccess RootUpgradeStatus = 1
	UpgradeFailure RootUpgradeStatus = 2
)

// checkAndRotateRootCert decides whether root cert should be refreshed, and rotates
// root cert for self-signed Citadel.
func (rotator *SelfSignedCARootCertRotator) checkAndRotateRootCert() {
	caSecret, scrtErr := rotator.caSecretController.LoadCASecretWithRetry(CASecret,
		rotator.caStorageNamespace, rotator.retryInterval, 30*time.Second)

	var status RootUpgradeStatus
	if scrtErr != nil {
		rootCertRotatorLog.Errorf("Fail to load CA secret %s:%s (error: %s), skip cert rotation job",
			rotator.caStorageNamespace, CASecret, scrtErr.Error())
		status = UpgradeSkip
	} else if rotator.readSigningCertOnly {
		status = rotator.checkAndRotateRootCertForReadOnlyCitadel(caSecret)
	} else {
		status = rotator.checkAndRotateRootCertForSigningCertCitadel(caSecret)
	}

	if status == UpgradeSuccess {
		rotator.metrics.RootUpgradeSuccess.Increment()
	}
	if status == UpgradeFailure {
		rotator.metrics.RootUpgradeErrors.Increment()
	}
}

func (rotator *SelfSignedCARootCertRotator) checkAndRotateRootCertForReadOnlyCitadel(
	caSecret *v1.Secret) RootUpgradeStatus {
	if caSecret == nil {
		rootCertRotatorLog.Info("Root cert does not exist, skip root cert rotation for " +
			"self-signed root cert read-only Citadel.")
		return UpgradeSkip
	}

	rootCertificate := rotator.ca.GetCAKeyCertBundle().GetRootCertPem()
	if !bytes.Equal(rootCertificate, caSecret.Data[RootCertID]) {
		// If the CA secret holds a different root cert than the root cert stored in
		// KeyCertBundle, this indicates that the local stored root cert is not
		// up-to-date. Update root cert and key in KeyCertBundle and config map.
		rootCertRotatorLog.Infof("Load signing key and cert from existing secret %s:%s", caSecret.Namespace, caSecret.Name)
		rootCerts, err := appendRootCerts(caSecret.Data[caCertID], rotator.rootCertFile)
		if err != nil {
			rootCertRotatorLog.Errorf("failed to append root certificates (%v)", err)
			return UpgradeFailure
		}
		if err := rotator.ca.GetCAKeyCertBundle().VerifyAndSetAll(caSecret.Data[caCertID],
			caSecret.Data[caPrivateKeyID], nil, rootCerts); err != nil {
			rootCertRotatorLog.Errorf("failed to create CA KeyCertBundle (%v)", err)
			return UpgradeFailure
		}
		rootCertRotatorLog.Infof("Updated CA KeyCertBundle using existing public key: %v", string(rootCerts))
		return UpgradeSuccess
		// For read-only self-signed CA instance, don't update root cert in config
		// map. The self-signed CA instance that updates root cert is responsible for
		// updating root cert in config map.
	}
	return UpgradeSkip
}

func (rotator *SelfSignedCARootCertRotator) checkAndRotateRootCertForSigningCertCitadel(
	caSecret *v1.Secret) RootUpgradeStatus {
	if caSecret == nil {
		rootCertRotatorLog.Errorf("root cert secret %s is nil, skip cert rotation job",
			CASecret)
		return UpgradeSkip
	}
	// Check root certificate expiration time in CA secret
	waitTime, err := rotator.certInspector.GetWaitTime(caSecret.Data[caCertID], time.Now(), time.Duration(0))
	if err == nil && waitTime > 0 {
		return UpgradeSkip
	}

	rootCertRotatorLog.Info("Refresh root certificate")
	options := util.CertOptions{
		TTL:           rotator.caCertTTL,
		SignerPrivPem: caSecret.Data[caPrivateKeyID],
		Org:           rotator.org,
		IsCA:          true,
		IsSelfSigned:  true,
		RSAKeySize:    caKeySize,
		IsDualUse:     rotator.dualUse,
	}
	pemCert, pemKey, ckErr := util.GenRootCertFromExistingKey(options)
	if ckErr != nil {
		rootCertRotatorLog.Errorf("unable to generate CA cert and key for self-signed CA: %s", ckErr.Error())
		return UpgradeFailure
	}

	rootCerts, err := appendRootCerts(pemCert, rotator.rootCertFile)
	if err != nil {
		rootCertRotatorLog.Errorf("failed to append root certificates: %s", err.Error())
		return UpgradeFailure
	}

	if err := rotator.ca.GetCAKeyCertBundle().VerifyAndSetAll(pemCert, pemKey, nil, rootCerts); err != nil {
		rootCertRotatorLog.Errorf("failed to create CA KeyCertBundle (%v)", err)
		return UpgradeFailure
	}

	caSecret.Data[caCertID] = pemCert
	caSecret.Data[caPrivateKeyID] = pemKey
	if err = rotator.caSecretController.UpdateCASecretWithRetry(caSecret, rotator.retryInterval, 30*time.Second); err != nil {
		rootCertRotatorLog.Errorf("Failed to write secret to CA secret (error: %s). "+
			"Abort new root certificate.", err.Error())
		return UpgradeFailure
	}
	rootCertRotatorLog.Infof("A new self-generated root certificate is written into secret: %v", string(rootCerts))

	certEncoded := base64.StdEncoding.EncodeToString(rotator.ca.GetCAKeyCertBundle().GetRootCertPem())
	if err = rotator.configMapController.InsertCATLSRootCertWithRetry(
		certEncoded, rotator.retryInterval, 30*time.Second); err != nil {
		rootCertRotatorLog.Errorf("Failed to write self-signed Citadel's root cert "+
			"to configmap (%s). Node agents will not be able to connect.",
			err.Error())
	}
	rootCertRotatorLog.Infof("Updated CA KeyCertBundle using existing public key: %v", string(rootCerts))
	return UpgradeSuccess
}
