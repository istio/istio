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
	"k8s.io/client-go/kubernetes/fake"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/istio/security/pkg/cmd"

	k8ssecret "istio.io/istio/security/pkg/k8s/secret"
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

	// Verifies that in read only mode, root cert rotator does not create CA secret.
	readOnlyCaOptions := getDefaultSelfSignedIstioCAOptions(nil)
	readOnlyCaOptions.RotatorConfig.readSigningCertOnly = true
	rotator1 := getRootCertRotator(readOnlyCaOptions)
	client1 := rotator1.config.client
	client1.Secrets(rotator1.config.caStorageNamespace).Delete(CASecret, &metav1.DeleteOptions{})

	rotator1.checkAndRotateRootCert()
	caSecret, err = client1.Secrets(rotator1.config.caStorageNamespace).Get(CASecret, metav1.GetOptions{})
	if !errors.IsNotFound(err) || caSecret != nil {
		t.Errorf("CA secret should not exist, but get %v: %v", caSecret, err)
	}
}

// TestRootCertRotatorForReadOnlyCitadel verifies that rotator updates local
// key cert bundle if root cert secret is rotated.
func TestRootCertRotatorForReadOnlyCitadel(t *testing.T) {
	client := fake.NewSimpleClientset().CoreV1()
	// Create CA secret
	signingCertPem := []byte(cert1Pem)
	signingKeyPem := []byte(key1Pem)
	caSecret := k8ssecret.BuildSecret("", CASecret, caNamespace,
		nil, nil, nil, signingCertPem, signingKeyPem, istioCASecretType)
	_, err := client.Secrets(caNamespace).Create(caSecret)
	if err != nil {
		t.Fatalf("Failed to create CA secret: %v", err)
	}

	// Create root cert rotator with the same k8s client.
ma	readOnlyCaOptions := getDefaultSelfSignedIstioCAOptions(client)
	readOnlyCaOptions.RotatorConfig.readSigningCertOnly = true
	rotator := getRootCertRotator(readOnlyCaOptions)

	// Verifies that the read-only Citadel has loaded root cert into key cert bundle.
	rootCertFromBundle := rotator.ca.keyCertBundle.GetRootCertPem()
	if !bytes.Equal(rootCertFromBundle, caSecret.Data[caCertID]) {
		t.Fatalf("Root cert in key cert bundle should match the root cert in CA secret. %v vs %v",
			rootCertFromBundle, caSecret.Data[caCertID])
	}

	// Verifies if CA secret is not changed, rotates root cert is no-op
	rotator.checkAndRotateRootCert()
	rootCertFromBundle1 := rotator.ca.keyCertBundle.GetRootCertPem()
	if !bytes.Equal(rootCertFromBundle1, rootCertFromBundle) {
		t.Errorf("Root cert in the key cert bundle should remain the same.")
	}

	// Make a copy of root cert from config map.
	rootCertInConfigMap, _ := rotator.configMapController.GetCATLSRootCert()

	// Change the root cert and private key in CA secret to let rotator rotate root cert.
	pemCert, pemKey, ckErr := util.GenCertKeyFromOptions(util.CertOptions{
		TTL:          1 * time.Hour,
		Org:          "org",
		IsCA:         true,
		IsSelfSigned: true,
		RSAKeySize:   caKeySize,
		IsDualUse:    false,
	})
	if ckErr != nil {
		t.Fatalf("Unable to generate CA cert and key for self-signed CA (%v)", ckErr)
	}
	caSecret.Data[caCertID] = pemCert
	caSecret.Data[caPrivateKeyID] = pemKey
	newCaSecret, err := client.Secrets(rotator.config.caStorageNamespace).Update(caSecret)
	if err != nil {
		t.Fatalf("Failed to update CA secret: %v", err)
	}
	if bytes.Equal(rootCertFromBundle, newCaSecret.Data[caCertID]) {
		t.Fatal("Root cert in key cert bundle should not match root cert in CA secret now")
	}

	// Verifies that after rotation, root cert in the key cert bundle has been updated.
	rotator.checkAndRotateRootCert()
	rootCertFromBundle = rotator.ca.keyCertBundle.GetRootCertPem()
	if !bytes.Equal(rootCertFromBundle, newCaSecret.Data[caCertID]) {
		t.Errorf("Root cert in the key cert bundle should have been updated")
	}

	// Because root cert rotator does not update the root cert in config map. That
	// root cert from config map should remain the same.
	rootCertInConfigMap2, _ := rotator.configMapController.GetCATLSRootCert()
	if rootCertInConfigMap != rootCertInConfigMap2 {
		t.Errorf("Unexpected root cert change in config map, %s vs %s",
			rootCertInConfigMap, rootCertInConfigMap2)
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
	readOnlyCaOptions := getDefaultSelfSignedIstioCAOptions(nil)
	readOnlyCaOptions.RotatorConfig.readSigningCertOnly = false
	rotator := getRootCertRotator(readOnlyCaOptions)

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
	readOnlyCaOptions := getDefaultSelfSignedIstioCAOptions(nil)
	readOnlyCaOptions.RotatorConfig.readSigningCertOnly = false
	rotator := getRootCertRotator(readOnlyCaOptions)

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

// TestRootCertRotatorGoroutineForSigningCitadel verifies that rotator
// periodically rotates root cert, updates key cert bundle and config map.
func TestRootCertRotatorGoroutineForSigningCitadel(t *testing.T) {
	readOnlyCaOptions := getDefaultSelfSignedIstioCAOptions(nil)
	readOnlyCaOptions.RotatorConfig.readSigningCertOnly = false
	rotator := getRootCertRotator(readOnlyCaOptions)

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

func getDefaultSelfSignedIstioCAOptions(client corev1.CoreV1Interface) *IstioCAOptions {
	caCertTTL := time.Hour
	defaultCertTTL := 30 * time.Minute
	maxCertTTL := time.Hour
	org := "test.ca.Org"
	if client == nil {
		client = fake.NewSimpleClientset().CoreV1()
	}
	rootCertFile := ""
	readSigningCertOnly := false
	rootCertCheckInverval := time.Hour

	caopts, _ := NewSelfSignedIstioCAOptions(context.Background(),
		readSigningCertOnly, cmd.DefaultRootCertGracePeriodPercentile, caCertTTL,
		rootCertCheckInverval, defaultCertTTL, maxCertTTL, org, false,
		caNamespace, -1, client, rootCertFile, false)
	return caopts
}

func getRootCertRotator(opts *IstioCAOptions) *SelfSignedCARootCertRotator {
	ca, _ := NewIstioCA(opts)
	return ca.rootCertRotator
}
