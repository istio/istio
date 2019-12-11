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
	"context"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"istio.io/istio/security/pkg/cmd"

	"istio.io/istio/security/pkg/pki/util"
	certutil "istio.io/istio/security/pkg/util"
)

const caNamespace = "default"

// TestJitterConfiguration tests the setup of jitter
func TestJitterConfiguration(t *testing.T) {
	enableJitterOpts := getDefaultSelfSignedIstioCAOptions(nil)
	enableJitterOpts.RotatorConfig.enableJitter = true
	rotator0 := getRootCertRotator(enableJitterOpts)
	if rotator0.backOffTime < time.Duration(0) {
		t.Errorf("back off time should be zero or positive but got %v", rotator0.backOffTime)
	}
	if rotator0.backOffTime >= rotator0.config.CheckInterval {
		t.Errorf("back off time should be shorter than rotation interval but got %v",
			rotator0.backOffTime)
	}

	disableJitterOpts := getDefaultSelfSignedIstioCAOptions(nil)
	disableJitterOpts.RotatorConfig.enableJitter = false
	rotator1 := getRootCertRotator(disableJitterOpts)
	if rotator1.backOffTime > time.Duration(0) {
		t.Errorf("back off time should be negative but got %v", rotator1.backOffTime)
	}
}

// TestRootCertRotatorWithoutRootCertSecret verifies that if root cert secret
// does not exist, the rotator does not add new root cert.
func TestRootCertRotatorWithoutRootCertSecret(t *testing.T) {
	// Verifies that in self-signed CA mode, root cert rotator does not create CA secret.
	rotator0 := getRootCertRotator(getDefaultSelfSignedIstioCAOptions(nil))
	client0 := rotator0.config.client
	client0.Secrets(rotator0.config.caStorageNamespace).Delete(CASecret, &metav1.DeleteOptions{})

	rotator0.checkAndRotateRootCert()
	caSecret, err := client0.Secrets(rotator0.config.caStorageNamespace).Get(CASecret, metav1.GetOptions{})
	if !errors.IsNotFound(err) || caSecret != nil {
		t.Errorf("CA secret should not exist, but get %v: %v", caSecret, err)
	}
}

type rootCertItem struct {
	caSecret                *v1.Secret
	rootCertInKeyCertBundle []byte
	rootCertInConfigMap     string
}

func verifyRootCertAndPrivateKey(t *testing.T, shouldMatch bool, itemA, itemB rootCertItem) {
	isMatched := bytes.Equal(itemA.caSecret.Data[caCertID], itemB.caSecret.Data[caCertID])
	if isMatched != shouldMatch {
		t.Errorf("Verification of root cert in CA secret failed. Want %v got %v", shouldMatch, isMatched)
	}
	isMatched = bytes.Equal(itemA.rootCertInKeyCertBundle, itemB.rootCertInKeyCertBundle)
	if isMatched != shouldMatch {
		t.Errorf("Verification of root cert in key cert bundle failed. Want %v got %v", shouldMatch, isMatched)
	}
	isMatched = (itemA.rootCertInConfigMap == itemB.rootCertInConfigMap)
	if isMatched != shouldMatch {
		t.Errorf("Verification of root cert in config map failed. Want %v got %v", shouldMatch, isMatched)
	}

	// Root cert rotation does not change root private key. Root private key should
	// remain the same.
	isMatched = bytes.Equal(itemA.caSecret.Data[caPrivateKeyID], itemB.caSecret.Data[caPrivateKeyID])
	if isMatched != true {
		t.Errorf("Root private key should not change. Want %v got %v", shouldMatch, isMatched)
	}
}

func loadCert(rotator *SelfSignedCARootCertRotator) rootCertItem {
	client := rotator.config.client
	caSecret, _ := client.Secrets(rotator.config.caStorageNamespace).Get(CASecret, metav1.GetOptions{})
	rootCert := rotator.ca.keyCertBundle.GetRootCertPem()
	rootCertInConfigMap, _ := rotator.configMapController.GetCATLSRootCert()
	return rootCertItem{caSecret: caSecret, rootCertInKeyCertBundle: rootCert,
		rootCertInConfigMap: rootCertInConfigMap}
}

// TestRootCertRotatorForSigningCitadel verifies that rotator rotates root cert,
// updates key cert bundle and config map.
func TestRootCertRotatorForSigningCitadel(t *testing.T) {
	rotator := getRootCertRotator(getDefaultSelfSignedIstioCAOptions(nil))

	// Make a copy of CA secret, a copy of root cert form key cert bundle, and
	// a copy of root cert from config map for verification.
	certItem0 := loadCert(rotator)

	// Change grace period percentage to 0, so that root cert is not going to expire soon.
	rotator.config.certInspector = certutil.NewCertUtil(0)
	rotator.checkAndRotateRootCert()
	// Verifies that when root cert remaining life is not in grace period time,
	// root cert is not rotated.
	certItem1 := loadCert(rotator)
	verifyRootCertAndPrivateKey(t, true, certItem0, certItem1)

	// Change grace period percentage to 100, so that root cert is guarantee to rotate.
	rotator.config.certInspector = certutil.NewCertUtil(100)
	rotator.checkAndRotateRootCert()
	certItem2 := loadCert(rotator)
	verifyRootCertAndPrivateKey(t, false, certItem1, certItem2)
}

// TestKeyCertBundleReloadInRootCertRotatorForSigningCitadel verifies that
// rotator reloads root cert into KeyCertBundle if the root cert in key cert bundle is
// different from istio-ca-secret.
func TestKeyCertBundleReloadInRootCertRotatorForSigningCitadel(t *testing.T) {
	rotator := getRootCertRotator(getDefaultSelfSignedIstioCAOptions(nil))

	// Mutate the root cert and private key as if they are rotated by other Citadel.
	certItem0 := loadCert(rotator)
	oldRootCert := certItem0.rootCertInKeyCertBundle
	options := util.CertOptions{
		TTL:           rotator.config.caCertTTL,
		SignerPrivPem: certItem0.caSecret.Data[caPrivateKeyID],
		Org:           rotator.config.org,
		IsCA:          true,
		IsSelfSigned:  true,
		RSAKeySize:    caKeySize,
		IsDualUse:     rotator.config.dualUse,
	}
	pemCert, pemKey, ckErr := util.GenRootCertFromExistingKey(options)
	if ckErr != nil {
		t.Fatalf("failed to rotate secret: %s", ckErr.Error())
	}
	newSecret := certItem0.caSecret
	newSecret.Data[caCertID] = pemCert
	newSecret.Data[caPrivateKeyID] = pemKey
	rotator.config.client.Secrets(rotator.config.caStorageNamespace).Update(newSecret)

	// Change grace period percentage to 0, so that root cert is not going to expire soon.
	rotator.config.certInspector = certutil.NewCertUtil(0)
	rotator.checkAndRotateRootCert()
	// Verifies that when root cert remaining life is not in grace period time,
	// root cert is not rotated.
	certItem1 := loadCert(rotator)
	if !bytes.Equal(newSecret.Data[caCertID], certItem1.caSecret.Data[caCertID]) {
		t.Error("root cert in istio-ca-secret should be the same.")
	}
	// Verifies that after rotation, the rotator should have reloaded root cert into
	// key cert bundle.
	if bytes.Equal(oldRootCert, rotator.ca.keyCertBundle.GetRootCertPem()) {
		t.Error("root cert in key cert bundle should be different after rotation.")
	}
	if !bytes.Equal(certItem1.caSecret.Data[caCertID], rotator.ca.keyCertBundle.GetRootCertPem()) {
		t.Error("root cert in key cert bundle should be the same as root " +
			"cert in istio-ca-secret after root cert rotation.")
	}
}

// TestRollbackAtRootCertRotatorForSigningCitadel verifies that rotator rollbacks
// new root cert if it fails to update new root cert into configmap.
func TestRollbackAtRootCertRotatorForSigningCitadel(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	rotator := getRootCertRotator(getDefaultSelfSignedIstioCAOptions(fakeClient))

	// Make a copy of CA secret, a copy of root cert form key cert bundle, and
	// a copy of root cert from config map for verification.
	certItem0 := loadCert(rotator)

	// Change grace period percentage to 100, so that root cert is guarantee to rotate.
	rotator.config.certInspector = certutil.NewCertUtil(100)
	fakeClient.PrependReactor("update", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &v1.ConfigMap{}, errors.NewUnauthorized("no permission to update configmap")
	})
	rotator.checkAndRotateRootCert()
	certItem1 := loadCert(rotator)
	// Verify that root cert does not change.
	verifyRootCertAndPrivateKey(t, true, certItem0, certItem1)
}

// TestRootCertRotatorGoroutineForSigningCitadel verifies that rotator
// periodically rotates root cert, updates key cert bundle and config map.
func TestRootCertRotatorGoroutineForSigningCitadel(t *testing.T) {
	rotator := getRootCertRotator(getDefaultSelfSignedIstioCAOptions(nil))

	// Make a copy of CA secret, a copy of root cert form key cert bundle, and
	// a copy of root cert from config map for verification.
	certItem0 := loadCert(rotator)

	// Configure rotator to periodically rotates root cert.
	rotator.config.certInspector = certutil.NewCertUtil(100)
	rotator.config.caCertTTL = 1 * time.Minute
	rotator.config.CheckInterval = 500 * time.Millisecond
	rootCertRotatorChan := make(chan struct{})
	go rotator.Run(rootCertRotatorChan)
	defer close(rootCertRotatorChan)

	// Wait until root cert rotation is done.
	time.Sleep(600 * time.Millisecond)
	certItem1 := loadCert(rotator)
	verifyRootCertAndPrivateKey(t, false, certItem0, certItem1)

	time.Sleep(600 * time.Millisecond)
	certItem2 := loadCert(rotator)
	verifyRootCertAndPrivateKey(t, false, certItem1, certItem2)
}

func getDefaultSelfSignedIstioCAOptions(fclient *fake.Clientset) *IstioCAOptions {
	caCertTTL := time.Hour
	defaultCertTTL := 30 * time.Minute
	maxCertTTL := time.Hour
	org := "test.ca.Org"
	client := fake.NewSimpleClientset().CoreV1()
	if fclient != nil {
		client = fclient.CoreV1()
	}
	rootCertFile := ""
	rootCertCheckInverval := time.Hour

	caopts, _ := NewSelfSignedIstioCAOptions(context.Background(),
		cmd.DefaultRootCertGracePeriodPercentile, caCertTTL,
		rootCertCheckInverval, defaultCertTTL, maxCertTTL, org, false,
		caNamespace, -1, client, rootCertFile, false)
	return caopts
}

func getRootCertRotator(opts *IstioCAOptions) *SelfSignedCARootCertRotator {
	ca, _ := NewIstioCA(opts)
	return ca.rootCertRotator
}
