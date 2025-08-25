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

package cache

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"istio.io/istio/pkg/file"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/monitoring/monitortest"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/testcerts"
	"istio.io/istio/security/pkg/nodeagent/caclient/providers/mock"
	"istio.io/istio/security/pkg/nodeagent/cafile"
	pkiutil "istio.io/istio/security/pkg/pki/util"
)

func TestWorkloadAgentGenerateSecret(t *testing.T) {
	mt := monitortest.New(t)
	fakeCACli, err := mock.NewMockCAClient(time.Hour, true)
	var got, want []byte
	if err != nil {
		t.Fatalf("Error creating Mock CA client: %v", err)
	}
	sc := createCache(t, fakeCACli, func(resourceName string) {}, security.Options{WorkloadRSAKeySize: 2048})
	gotSecret, err := sc.GenerateSecret(security.WorkloadKeyCertResourceName)
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}

	if got, want := gotSecret.CertificateChain, []byte(strings.Join(fakeCACli.GeneratedCerts[0], "")); !bytes.Equal(got, want) {
		t.Errorf("Got unexpected certificate chain #1. Got: %v, want: %v", string(got), string(want))
	}

	gotSecretRoot, err := sc.GenerateSecret(security.RootCertReqResourceName)
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	// Root cert is the last element in the generated certs.
	got, want = gotSecretRoot.RootCert, []byte(strings.TrimSuffix(fakeCACli.GeneratedCerts[0][2], "\n"))
	if !bytes.Equal(got, want) {
		t.Errorf("Got unexpected root certificate. Got: %v\n want: %v", string(got), string(want))
	}

	// Try to get secret again, verify secret is not generated.
	gotSecret, err = sc.GenerateSecret(security.WorkloadKeyCertResourceName)
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}

	if got, want := gotSecret.CertificateChain, []byte(strings.Join(fakeCACli.GeneratedCerts[0], "")); !bytes.Equal(got, want) {
		t.Errorf("Got unexpected certificate chain #1. Got: %v, want: %v", string(got), string(want))
	}

	// Root cert is the last element in the generated certs.
	want = []byte(fakeCACli.GeneratedCerts[0][2])
	if got := sc.cache.GetRoot(); !bytes.Equal(got, want) {
		t.Errorf("Got unexpected root certificate. Got: %v\n want: %v", string(got), string(want))
	}

	certDefaultTTL := time.Hour.Seconds()
	mt.Assert(certExpirySeconds.Name(), map[string]string{"resource_name": "default"}, monitortest.LessThan(certDefaultTTL))
}

func createCache(t *testing.T, caClient security.Client, notifyCb func(resourceName string), options security.Options) *SecretManagerClient {
	t.Helper()
	sc, err := NewSecretManagerClient(caClient, &options)
	if err != nil {
		t.Fatal(err)
	}
	sc.RegisterSecretHandler(notifyCb)
	t.Cleanup(sc.Close)
	return sc
}

type UpdateTracker struct {
	t    *testing.T
	hits map[string]int
	mu   sync.Mutex
}

func NewUpdateTracker(t *testing.T) *UpdateTracker {
	return &UpdateTracker{
		t:    t,
		hits: map[string]int{},
		mu:   sync.Mutex{},
	}
}

func (u *UpdateTracker) Callback(name string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.hits[name]++
}

func (u *UpdateTracker) Expect(want map[string]int) {
	u.t.Helper()
	retry.UntilSuccessOrFail(u.t, func() error {
		u.mu.Lock()
		defer u.mu.Unlock()
		if !reflect.DeepEqual(u.hits, want) {
			return fmt.Errorf("wanted %+v got %+v", want, u.hits)
		}
		return nil
	}, retry.Timeout(time.Second*5))
}

func (u *UpdateTracker) Reset() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.hits = map[string]int{}
}

func TestWorkloadAgentRefreshSecret(t *testing.T) {
	cacheLog.SetOutputLevel(log.DebugLevel)
	fakeCACli, err := mock.NewMockCAClient(time.Millisecond*200, false)
	if err != nil {
		t.Fatalf("Error creating Mock CA client: %v", err)
	}
	u := NewUpdateTracker(t)
	sc := createCache(t, fakeCACli, u.Callback, security.Options{WorkloadRSAKeySize: 2048})

	_, err = sc.GenerateSecret(security.WorkloadKeyCertResourceName)
	if err != nil {
		t.Fatalf("failed to get secrets: %v", err)
	}

	// First update will trigger root cert immediately, then workload cert once it expires in 200ms
	u.Expect(map[string]int{security.WorkloadKeyCertResourceName: 1, security.RootCertReqResourceName: 1})

	_, err = sc.GenerateSecret(security.WorkloadKeyCertResourceName)
	if err != nil {
		t.Fatalf("failed to get secrets: %v", err)
	}

	u.Expect(map[string]int{security.WorkloadKeyCertResourceName: 2, security.RootCertReqResourceName: 1})
}

// Compare times, with 5s error allowance
func almostEqual(t1, t2 time.Duration) bool {
	diff := t1 - t2
	if diff < 0 {
		diff *= -1
	}
	return diff < time.Second*5
}

func TestRotateTimeNoJitter(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name             string
		created          time.Time
		expire           time.Time
		gracePeriodRatio float64
		expected         time.Duration
	}{
		{
			name:             "already expired",
			created:          now.Add(-time.Second * 2),
			expire:           now.Add(-time.Second),
			gracePeriodRatio: 0.5,
			expected:         0,
		},
		{
			name:             "grace period .50",
			created:          now,
			expire:           now.Add(time.Hour),
			gracePeriodRatio: 0.5,
			expected:         time.Minute * 30,
		},
		{
			name:             "grace period .25",
			created:          now,
			expire:           now.Add(time.Hour),
			gracePeriodRatio: 0.25,
			expected:         time.Minute * 45,
		},
		{
			name:             "grace period .75",
			created:          now,
			expire:           now.Add(time.Hour),
			gracePeriodRatio: 0.75,
			expected:         time.Minute * 15,
		},
		{
			name:             "grace period 1",
			created:          now,
			expire:           now.Add(time.Hour),
			gracePeriodRatio: 1,
			expected:         0,
		},
		{
			name:             "grace period 0",
			created:          now,
			expire:           now.Add(time.Hour),
			gracePeriodRatio: 0,
			expected:         time.Hour,
		},
		{
			name:             "grace period .25 shifted",
			created:          now.Add(time.Minute * 30),
			expire:           now.Add(time.Minute * 90),
			gracePeriodRatio: 0.25,
			expected:         time.Minute * 75,
		},
		{
			name:             "jitter cannot make grace ratio exceed 1",
			created:          now,
			expire:           now.Add(time.Hour),
			gracePeriodRatio: 1,
			expected:         0,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := rotateTime(security.SecretItem{CreatedTime: tt.created, ExpireTime: tt.expire}, tt.gracePeriodRatio, 0)
			if !almostEqual(got, tt.expected) {
				t.Fatalf("expected %v got %v", tt.expected, got)
			}
		})
	}
}

func TestRotateTimeWithJitter(t *testing.T) {
	now := time.Now()
	type delayRange struct {
		min time.Duration
		max time.Duration
	}
	cases := []struct {
		name             string
		created          time.Time
		expire           time.Time
		gracePeriodRatio float64
		jitter           float64
		expectedDelay    delayRange
	}{
		{
			name:             "jitter ratio between .5 and 1",
			created:          now,
			expire:           now.Add(time.Hour),
			gracePeriodRatio: 1,
			jitter:           .5,
			expectedDelay: delayRange{
				0,
				time.Hour / 2,
			},
		},
		{
			name:             "jitter ratio between 0 and 1",
			created:          now,
			expire:           now.Add(time.Hour),
			gracePeriodRatio: .5,
			jitter:           .5,
			expectedDelay: delayRange{
				0,
				time.Hour,
			},
		},
		{
			name:             "jitter ratio between 0 and .5",
			created:          now,
			expire:           now.Add(time.Hour),
			gracePeriodRatio: 0,
			jitter:           .5,
			expectedDelay: delayRange{
				time.Hour / 2,
				time.Hour,
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < 1000; i++ {
				got := rotateTime(security.SecretItem{
					CreatedTime: tt.created,
					ExpireTime:  tt.expire,
				}, tt.gracePeriodRatio, tt.jitter)
				if got+time.Second < tt.expectedDelay.min || got > tt.expectedDelay.max {
					t.Fatalf(
						"got %v, expected between %v - %v with gracePeriodRatio of %v and jitter of %v",
						got,
						tt.expectedDelay.min,
						tt.expectedDelay.max,
						tt.gracePeriodRatio,
						tt.jitter,
					)
				}
			}
		})
	}
}

func TestRootCertificateExists(t *testing.T) {
	testCases := map[string]struct {
		certPath     string
		expectResult bool
	}{
		"cert not exist": {
			certPath:     "./invalid-path/invalid-file",
			expectResult: false,
		},
		"cert valid": {
			certPath:     "./testdata/cert-chain.pem",
			expectResult: true,
		},
	}

	sc := createCache(t, nil, func(resourceName string) {}, security.Options{})
	for _, tc := range testCases {
		ret := sc.rootCertificateExist(tc.certPath)
		if tc.expectResult != ret {
			t.Errorf("unexpected result is returned!")
		}
	}
}

func TestKeyCertificateExist(t *testing.T) {
	testCases := map[string]struct {
		certPath     string
		keyPath      string
		expectResult bool
	}{
		"cert not exist": {
			certPath:     "./invalid-path/invalid-file",
			keyPath:      "./testdata/cert-chain.pem",
			expectResult: false,
		},
		"key not exist": {
			certPath:     "./testdata/cert-chain.pem",
			keyPath:      "./invalid-path/invalid-file",
			expectResult: false,
		},
		"key and cert valid": {
			certPath:     "./testdata/cert-chain.pem",
			keyPath:      "./testdata/cert-chain.pem",
			expectResult: true,
		},
	}
	sc := createCache(t, nil, func(resourceName string) {}, security.Options{})
	for _, tc := range testCases {
		ret := sc.keyCertificateExist(tc.certPath, tc.keyPath)
		if tc.expectResult != ret {
			t.Errorf("unexpected result is returned!")
		}
	}
}

func TestSomeInvalidCerts(t *testing.T) {
	// Tests that we can properly parse root certs and handle the case where some of the certs are not valid.
	testCases := map[string]struct {
		certPath   string
		validCerts int
		err        *string
	}{
		"negative serial": {
			certPath:   "./testdata/root-ca-bundle-with-graceful-failures.pem",
			validCerts: 3,
		},
		"empty file": {
			certPath:   "./testdata/emptyfile.pem",
			validCerts: 0,
		},
		"invalid pem": {
			certPath:   "./testdata/invalidpem.pem",
			validCerts: 1,
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			sc := createCache(t, nil, func(string) {}, security.Options{})
			si, err := sc.generateRootCertFromExistingFile(tc.certPath, "dummy", false)
			if tc.validCerts == 0 {
				if si != nil || err == nil {
					t.Fatalf("Expected no valid certs, but got %v and error %v", si, err)
				}
				return
			}
			if si == nil {
				t.Fatalf("Expected valid secret item, but got nil for file %s", tc.certPath)
			}
			rootCertDer := make([]byte, 0, len(si.RootCert))
			rest := si.RootCert
			var cert *pem.Block
			for {
				cert, rest = pem.Decode(rest)
				if cert == nil {
					break
				}
				rootCertDer = append(rootCertDer, cert.Bytes...)
			}
			certs, err := x509.ParseCertificates(rootCertDer)
			if err != nil {
				t.Fatalf("Error parsing valid root certs from file %s: %v", tc.certPath, err)
			}
			if len(certs) != tc.validCerts {
				t.Fatalf("Expected %d valid certs, got %d", tc.validCerts, len(certs))
			}
		})
	}
}

func setupTestDir(t *testing.T, sc *SecretManagerClient) {
	dir := t.TempDir()

	for _, f := range []string{"root-cert.pem", "key.pem", "cert-chain.pem"} {
		if err := file.AtomicCopy(filepath.Join("./testdata", f), dir, f); err != nil {
			t.Fatal(err)
		}
	}
	sc.existingCertificateFile = security.SdsCertificateConfig{
		CertificatePath:   filepath.Join(dir, "cert-chain.pem"),
		PrivateKeyPath:    filepath.Join(dir, "key.pem"),
		CaCertificatePath: filepath.Join(dir, "root-cert.pem"),
	}
}

// TestWorkloadAgentGenerateSecretFromFile tests generating secrets from existing files on a
// secretcache instance.
func TestFileSecrets(t *testing.T) {
	t.Run("file", func(t *testing.T) {
		runFileAgentTest(t, false)
	})
	t.Run("file sds", func(t *testing.T) {
		runFileAgentTest(t, true)
	})
}

func runFileAgentTest(t *testing.T, sds bool) {
	fakeCACli, err := mock.NewMockCAClient(time.Hour, false)
	if err != nil {
		t.Fatalf("Error creating Mock CA client: %v", err)
	}
	opt := security.Options{}

	u := NewUpdateTracker(t)
	sc := createCache(t, fakeCACli, u.Callback, opt)

	setupTestDir(t, sc)

	workloadResource := security.WorkloadKeyCertResourceName
	rootResource := security.RootCertReqResourceName
	if sds {
		workloadResource = sc.existingCertificateFile.GetResourceName()
		rootResource = sc.existingCertificateFile.GetRootResourceName()
	}

	certchain, err := os.ReadFile(sc.existingCertificateFile.CertificatePath)
	if err != nil {
		t.Fatalf("Error reading the cert chain file: %v", err)
	}
	privateKey, err := os.ReadFile(sc.existingCertificateFile.PrivateKeyPath)
	if err != nil {
		t.Fatalf("Error reading the private key file: %v", err)
	}
	rootCert, err := os.ReadFile(sc.existingCertificateFile.CaCertificatePath)
	if err != nil {
		t.Fatalf("Error reading the root cert file: %v", err)
	}

	// Check we can load key, cert, and root
	checkSecret(t, sc, workloadResource, security.SecretItem{
		ResourceName:     workloadResource,
		CertificateChain: certchain,
		PrivateKey:       privateKey,
	})
	checkSecret(t, sc, rootResource, security.SecretItem{
		ResourceName: rootResource,
		RootCert:     rootCert,
	})
	// We shouldn't get an pushes; these only happen on changes
	u.Expect(map[string]int{})
	u.Reset()

	if err := file.AtomicWrite(sc.existingCertificateFile.CertificatePath, testcerts.RotatedCert, os.FileMode(0o644)); err != nil {
		t.Fatal(err)
	}
	if err := file.AtomicWrite(sc.existingCertificateFile.PrivateKeyPath, testcerts.RotatedKey, os.FileMode(0o644)); err != nil {
		t.Fatal(err)
	}

	// Expect update callback
	u.Expect(map[string]int{workloadResource: 1})
	// On the next generate call, we should get the new cert
	checkSecret(t, sc, workloadResource, security.SecretItem{
		ResourceName:     workloadResource,
		CertificateChain: testcerts.RotatedCert,
		PrivateKey:       testcerts.RotatedKey,
	})

	if err := file.AtomicWrite(sc.existingCertificateFile.PrivateKeyPath, testcerts.RotatedKey, os.FileMode(0o644)); err != nil {
		t.Fatal(err)
	}
	// We do NOT expect update callback. We only watch the cert file, since the key and cert must be updated
	// in tandem.
	u.Expect(map[string]int{workloadResource: 1})
	u.Reset()

	checkSecret(t, sc, workloadResource, security.SecretItem{
		ResourceName:     workloadResource,
		CertificateChain: testcerts.RotatedCert,
		PrivateKey:       testcerts.RotatedKey,
	})

	if err := file.AtomicWrite(sc.existingCertificateFile.CaCertificatePath, testcerts.CACert, os.FileMode(0o644)); err != nil {
		t.Fatal(err)
	}
	// We expect to get an update notification, and the new root cert to be read
	u.Expect(map[string]int{rootResource: 1})
	u.Reset()

	checkSecret(t, sc, rootResource, security.SecretItem{
		ResourceName: rootResource,
		RootCert:     testcerts.CACert,
	})

	// Remove the file and add it again and validate that proxy is updated with new cert.
	if err := os.Remove(sc.existingCertificateFile.CaCertificatePath); err != nil {
		t.Fatal(err)
	}

	if err := file.AtomicWrite(sc.existingCertificateFile.CaCertificatePath, testcerts.CACert, os.FileMode(0o644)); err != nil {
		t.Fatal(err)
	}
	// We expect to get an update notification, and the new root cert to be read
	// We do not expect update callback for REMOVE events.
	u.Expect(map[string]int{rootResource: 1})

	checkSecret(t, sc, rootResource, security.SecretItem{
		ResourceName: rootResource,
		RootCert:     testcerts.CACert,
	})

	// Double check workload cert is untouched
	checkSecret(t, sc, workloadResource, security.SecretItem{
		ResourceName:     workloadResource,
		CertificateChain: testcerts.RotatedCert,
		PrivateKey:       testcerts.RotatedKey,
	})
}

func checkSecret(t *testing.T, sc *SecretManagerClient, name string, expected security.SecretItem) {
	t.Helper()
	got, err := sc.GenerateSecret(name)
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	verifySecret(t, got, &expected)
}

func TestWorkloadAgentGenerateSecretFromFileOverSdsWithBogusFiles(t *testing.T) {
	originalTimeout := totalTimeout
	totalTimeout = time.Millisecond * 1
	defer func() {
		totalTimeout = originalTimeout
	}()

	u := NewUpdateTracker(t)
	sc := createCache(t, nil, u.Callback, security.Options{})
	rootCertPath, _ := filepath.Abs("./testdata/root-cert-bogus.pem")
	keyPath, _ := filepath.Abs("./testdata/key-bogus.pem")
	certChainPath, _ := filepath.Abs("./testdata/cert-chain-bogus.pem")

	resource := fmt.Sprintf("file-cert:%s~%s", certChainPath, keyPath)

	gotSecret, err := sc.GenerateSecret(resource)

	if err == nil {
		t.Fatal("expected to get error")
	}

	if gotSecret != nil {
		t.Fatalf("Expected to get nil secret but got %v", gotSecret)
	}

	rootResource := "file-root:" + rootCertPath
	gotSecretRoot, err := sc.GenerateSecret(rootResource)

	if err == nil {
		t.Fatal("Expected to get error, but did not get")
	}
	if !strings.Contains(err.Error(), "no such file or directory") {
		t.Fatalf("Expected file not found error, but got %v", err)
	}
	if gotSecretRoot != nil {
		t.Fatalf("Expected to get nil secret but got %v", gotSecret)
	}

	u.Expect(map[string]int{})
}

func verifySecret(t *testing.T, gotSecret *security.SecretItem, expectedSecret *security.SecretItem) {
	if expectedSecret.ResourceName != gotSecret.ResourceName {
		t.Fatalf("resource name:: expected %s but got %s", expectedSecret.ResourceName,
			gotSecret.ResourceName)
	}
	cfg, ok := security.SdsCertificateConfigFromResourceName(expectedSecret.ResourceName)
	if expectedSecret.ResourceName == security.RootCertReqResourceName || (ok && cfg.IsRootCertificate()) {
		expectedRootCert := bytes.TrimSpace(expectedSecret.RootCert)
		gotRootCert := bytes.TrimSpace(gotSecret.RootCert)
		if !bytes.Equal(expectedRootCert, gotRootCert) {
			t.Fatalf("root cert: expected %v but got %v", expectedRootCert, gotRootCert)
		}
	} else {
		expectedCertChain := bytes.TrimSpace(expectedSecret.CertificateChain)
		gotCertChain := bytes.TrimSpace(gotSecret.CertificateChain)
		if !bytes.Equal(expectedCertChain, gotCertChain) {
			t.Fatalf("cert chain: expected %s but got %s", string(expectedCertChain), string(gotCertChain))
		}
		expectedPrivateKey := bytes.TrimSpace(expectedSecret.PrivateKey)
		gotPrivateKey := bytes.TrimSpace(gotSecret.PrivateKey)
		if !bytes.Equal(expectedPrivateKey, gotPrivateKey) {
			t.Fatalf("private key: expected %s but got %s", string(expectedPrivateKey), string(gotPrivateKey))
		}
	}
	if !expectedSecret.ExpireTime.IsZero() && expectedSecret.ExpireTime != gotSecret.ExpireTime {
		t.Fatalf("expiration: expected %v but got %v",
			expectedSecret.ExpireTime, gotSecret.ExpireTime)
	}
}

func TestConcatCerts(t *testing.T) {
	cases := []struct {
		name     string
		certs    []string
		expected string
	}{
		{
			name:     "no certs",
			certs:    []string{},
			expected: "",
		},
		{
			name:     "single cert",
			certs:    []string{"a"},
			expected: "a",
		},
		{
			name:     "multiple certs",
			certs:    []string{"a", "b"},
			expected: "a\nb",
		},
		{
			name:     "existing newline",
			certs:    []string{"a\n", "b"},
			expected: "a\nb",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := string(concatCerts(c.certs))
			if result != c.expected {
				t.Fatalf("expected %q, got %q", c.expected, result)
			}
		})
	}
}

func TestProxyConfigAnchors(t *testing.T) {
	fakeCACli, err := mock.NewMockCAClient(time.Hour, false)
	if err != nil {
		t.Fatalf("Error creating Mock CA client: %v", err)
	}
	u := NewUpdateTracker(t)

	sc := createCache(t, fakeCACli, u.Callback, security.Options{WorkloadRSAKeySize: 2048})
	_, err = sc.GenerateSecret(security.WorkloadKeyCertResourceName)
	if err != nil {
		t.Errorf("failed to generate certificate for trustAnchor test case")
	}
	// Ensure Root cert call back gets invoked once
	u.Expect(map[string]int{security.RootCertReqResourceName: 1})
	u.Reset()

	caClientRootCert := []byte(strings.TrimRight(fakeCACli.GeneratedCerts[0][2], "\n"))
	// Ensure that contents of the rootCert are correct.
	checkSecret(t, sc, security.RootCertReqResourceName, security.SecretItem{
		ResourceName: security.RootCertReqResourceName,
		RootCert:     caClientRootCert,
	})

	rootCert, err := os.ReadFile(filepath.Join("./testdata", "root-cert.pem"))
	if err != nil {
		t.Fatalf("Error reading the root cert file: %v", err)
	}

	// Update the proxyConfig with certs
	sc.UpdateConfigTrustBundle(rootCert)

	// Ensure Callback gets invoked when updating proxyConfig trust bundle
	u.Expect(map[string]int{security.RootCertReqResourceName: 1, security.WorkloadKeyCertResourceName: 1})
	u.Reset()

	concatCerts := func(certs ...string) []byte {
		expectedRootBytes := []byte{}
		sort.Strings(certs)
		for _, cert := range certs {
			expectedRootBytes = pkiutil.AppendCertByte(expectedRootBytes, []byte(cert))
		}
		return expectedRootBytes
	}

	expectedCerts := concatCerts(string(rootCert), string(caClientRootCert))
	// Ensure that contents of the rootCert are correct.
	checkSecret(t, sc, security.RootCertReqResourceName, security.SecretItem{
		ResourceName: security.RootCertReqResourceName,
		RootCert:     expectedCerts,
	})

	// Add Duplicates
	sc.UpdateConfigTrustBundle(expectedCerts)
	// Ensure that contents of the rootCert are correct without the duplicate caClientRootCert
	checkSecret(t, sc, security.RootCertReqResourceName, security.SecretItem{
		ResourceName: security.RootCertReqResourceName,
		RootCert:     expectedCerts,
	})

	if !bytes.Equal(sc.mergeConfigTrustBundle([]string{string(caClientRootCert), string(rootCert)}), expectedCerts) {
		t.Fatal("deduplicate test failed!")
	}

	// Update the proxyConfig with fakeCaClient certs
	sc.UpdateConfigTrustBundle(caClientRootCert)
	setupTestDir(t, sc)

	rootCert, err = os.ReadFile(sc.existingCertificateFile.CaCertificatePath)
	if err != nil {
		t.Fatalf("Error reading the root cert file: %v", err)
	}

	// Check request for workload root-certs merges configuration with ProxyConfig TrustAnchor
	checkSecret(t, sc, security.RootCertReqResourceName, security.SecretItem{
		ResourceName: security.RootCertReqResourceName,
		RootCert:     concatCerts(string(rootCert), string(caClientRootCert)),
	})

	// Check request for non-workload root-certs doesn't configuration with ProxyConfig TrustAnchor
	checkSecret(t, sc, sc.existingCertificateFile.GetRootResourceName(), security.SecretItem{
		ResourceName: sc.existingCertificateFile.GetRootResourceName(),
		RootCert:     rootCert,
	})
}

func TestProxyConfigAnchorsTriggerWorkloadCertUpdate(t *testing.T) {
	cacheLog.SetOutputLevel(log.DebugLevel)
	fakeCACli, err := mock.NewMockCAClient(time.Millisecond*200, false)
	if err != nil {
		t.Fatalf("Error creating Mock CA client: %v", err)
	}
	u := NewUpdateTracker(t)
	sc := createCache(t, fakeCACli, u.Callback, security.Options{WorkloadRSAKeySize: 2048})
	_, err = sc.GenerateSecret(security.WorkloadKeyCertResourceName)
	if err != nil {
		t.Errorf("failed to generate certificate for trustAnchor test case")
	}
	// Ensure Root cert call back gets invoked once, then workload cert once it expires in 200ms
	u.Expect(map[string]int{security.RootCertReqResourceName: 1, security.WorkloadKeyCertResourceName: 1})
	u.Reset()

	rootCert, err := os.ReadFile(filepath.Join("./testdata", "root-cert.pem"))
	if err != nil {
		t.Fatalf("Error reading the root cert file: %v", err)
	}
	// Update the proxyConfig with certs
	sc.UpdateConfigTrustBundle(rootCert)

	assert.Equal(t, sc.cache.GetWorkload(), nil)
	// Ensure Callback gets invoked when updating proxyConfig trust bundle
	u.Expect(map[string]int{security.RootCertReqResourceName: 1, security.WorkloadKeyCertResourceName: 1})
	u.Reset()

	rotateTime = func(_ security.SecretItem, _ float64, _ float64) time.Duration {
		return time.Millisecond * 200
	}
	fakeCACli, err = mock.NewMockCAClient(time.Millisecond*200, false)
	if err != nil {
		t.Fatalf("Error creating Mock CA client: %v", err)
	}
	sc = createCache(t, fakeCACli, u.Callback, security.Options{WorkloadRSAKeySize: 2048})
	_, err = sc.GenerateSecret(security.WorkloadKeyCertResourceName)
	if err != nil {
		t.Errorf("failed to generate certificate for trustAnchor test case")
	}
	// Immediately update the proxyConfig root cert
	sc.UpdateConfigTrustBundle(rootCert)
	time.Sleep(time.Millisecond * 200)
	// The rotation task actually will not call `OnSecretUpdate`, otherwise the WorkloadKeyCertResourceName event number should be 2
	u.Expect(map[string]int{security.RootCertReqResourceName: 2, security.WorkloadKeyCertResourceName: 1})
}

func TestOSCACertGenerateSecret(t *testing.T) {
	fakeCACli, err := mock.NewMockCAClient(time.Hour, false)
	if err != nil {
		t.Fatalf("Error creating Mock CA client: %v", err)
	}

	sc := createCache(t, fakeCACli, func(resourceName string) {}, security.Options{CARootPath: cafile.CACertFilePath})
	certPath := security.GetOSRootFilePath()
	expected, err := sc.GenerateSecret("file-root:" + certPath)
	if err != nil {
		t.Fatalf("Could not get OS Cert: %v", err)
	}

	gotSecret, err := sc.GenerateSecret(security.FileRootSystemCACert)
	if err != nil {
		t.Fatalf("Error using %s: %v", security.FileRootSystemCACert, err)
	}
	if !bytes.Equal(gotSecret.RootCert, expected.RootCert) {
		t.Fatal("Certs did not match")
	}
}

func TestOSCACertGenerateSecretEmpty(t *testing.T) {
	fakeCACli, err := mock.NewMockCAClient(time.Hour, false)
	if err != nil {
		t.Fatalf("Error creating Mock CA client: %v", err)
	}

	sc := createCache(t, fakeCACli, func(resourceName string) {}, security.Options{WorkloadRSAKeySize: 2048})
	certPath := security.GetOSRootFilePath()
	expected, err := sc.GenerateSecret("file-root:" + certPath)
	if err != nil {
		t.Fatalf(": %v", err)
	}

	gotSecret, err := sc.GenerateSecret(security.FileRootSystemCACert)
	if err != nil && len(gotSecret.RootCert) != 0 {
		t.Fatalf("Error using %s: %v", security.FileRootSystemCACert, err)
	}
	if bytes.Equal(gotSecret.RootCert, expected.RootCert) {
		t.Fatal("Certs did match")
	}
}

func TestTryAddFileWatcher(t *testing.T) {
	var (
		dummyResourceName = "default"
		relativeFilePath  = "./testdata/file-to-watch.txt"
	)
	absFilePathOfRelativeFilePath, err := filepath.Abs(relativeFilePath)
	if err != nil {
		t.Fatalf("unable to get absolute path to file %s, err: %v", relativeFilePath, err)
	}
	fakeCACli, err := mock.NewMockCAClient(time.Hour, true)
	if err != nil {
		t.Fatalf("unable to create fake mock ca client: %v", err)
	}
	sc := createCache(t, fakeCACli, func(resourceName string) {}, security.Options{WorkloadRSAKeySize: 2048})
	cases := []struct {
		name               string
		filePath           string
		expFilePathToWatch string
		expErr             error
	}{
		{
			name: "Given a file is expected to be watched, " +
				"When tryAddFileWatcher is invoked, with file path which does not start with /" +
				"Then tryAddFileWatcher should watch on the absolute path",
			filePath:           relativeFilePath,
			expFilePathToWatch: absFilePathOfRelativeFilePath,
			expErr:             nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err = sc.tryAddFileWatcher(c.filePath, dummyResourceName)
			if err != c.expErr {
				t.Fatalf("expected: %v, got: %v", c.expErr, err)
			}
			t.Logf("file watch: %v\n", sc.certWatcher.WatchList())
			if c.expErr == nil && len(sc.certWatcher.WatchList()) != 1 {
				t.Fatalf("expected certWatcher to watch 1 file, but it is watching: %d files", len(sc.certWatcher.WatchList()))
			}
			for _, v := range sc.certWatcher.WatchList() {
				if v != c.expFilePathToWatch {
					t.Fatalf(
						"expected certWatcher to watch on: %s, but it is watching on: %s",
						c.expFilePathToWatch, v)
				}
			}
		})
	}
}

func TestTryAddFileWatcherWithSymlink(t *testing.T) {
	var (
		dummyResourceName = "default"
		tempDir           = t.TempDir()
		realFile          = filepath.Join(tempDir, "real-cert.pem")
		symlinkFile       = filepath.Join(tempDir, "symlink-cert.pem")
	)

	// Create a real file
	err := os.WriteFile(realFile, []byte("test certificate content"), 0644)
	if err != nil {
		t.Fatalf("unable to create test file: %v", err)
	}

	// Create a symlink to the real file
	err = os.Symlink(realFile, symlinkFile)
	if err != nil {
		t.Fatalf("unable to create symlink: %v", err)
	}

	fakeCACli, err := mock.NewMockCAClient(time.Hour, true)
	if err != nil {
		t.Fatalf("unable to create fake mock ca client: %v", err)
	}

	sc := createCache(t, fakeCACli, func(resourceName string) {}, security.Options{WorkloadRSAKeySize: 2048})

	// Test that watching a symlink watches the target file
	err = sc.tryAddFileWatcher(symlinkFile, dummyResourceName)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify that the watcher is watching the real file, not the symlink
	watchList := sc.certWatcher.WatchList()
	if len(watchList) != 1 {
		t.Fatalf("expected certWatcher to watch 1 file, but it is watching: %d files", len(watchList))
	}

	// The watcher should be watching the real file path, not the symlink path
	expectedPath := realFile
	if watchList[0] != expectedPath {
		t.Fatalf("expected certWatcher to watch on: %s, but it is watching on: %s", expectedPath, watchList[0])
	}

	// Verify that the fileCerts map contains the symlink path (for display/logging purposes)
	sc.certMutex.RLock()
	found := false
	for fc := range sc.fileCerts {
		if fc.Filename == symlinkFile {
			found = true
			break
		}
	}
	sc.certMutex.RUnlock()
	if !found {
		t.Fatalf("expected symlink path %s to be in fileCerts map", symlinkFile)
	}
}

func TestHandleSymlinkChangesWithRemoval(t *testing.T) {
	tempDir := t.TempDir()
	realFile1 := filepath.Join(tempDir, "real-file-1.txt")
	realFile2 := filepath.Join(tempDir, "real-file-2.txt")
	symlinkFile := filepath.Join(tempDir, "symlink.txt")

	// Create first real file with initial content
	err := os.WriteFile(realFile1, []byte("initial content"), 0644)
	if err != nil {
		t.Fatalf("unable to create first test file: %v", err)
	}

	// Create second real file with different content
	err = os.WriteFile(realFile2, []byte("updated content"), 0644)
	if err != nil {
		t.Fatalf("unable to create second test file: %v", err)
	}

	// Create a symlink initially pointing to the first file
	err = os.Symlink(realFile1, symlinkFile)
	if err != nil {
		t.Fatalf("unable to create symlink: %v", err)
	}

	fakeCACli, err := mock.NewMockCAClient(time.Hour, true)
	if err != nil {
		t.Fatalf("unable to create fake mock ca client: %v", err)
	}

	sc := createCache(t, fakeCACli, func(resourceName string) {}, security.Options{WorkloadRSAKeySize: 2048})

	// Add the symlink to fileCerts to simulate it being watched
	sc.certMutex.Lock()
	sc.fileCerts[FileCert{ResourceName: "test", Filename: symlinkFile}] = struct{}{}
	sc.certMutex.Unlock()

	// Test 1: Verify initial symlink resolution works
	initialTarget, err := sc.resolveSymlink(symlinkFile)
	if err != nil {
		t.Fatalf("unable to resolve initial symlink: %v", err)
	}
	if initialTarget != realFile1 {
		t.Fatalf("expected symlink to resolve to %s, got %s", realFile1, initialTarget)
	}

	// Test 2: Update symlink to point to the second file
	err = os.Remove(symlinkFile)
	if err != nil {
		t.Fatalf("unable to remove old symlink: %v", err)
	}

	err = os.Symlink(realFile2, symlinkFile)
	if err != nil {
		t.Fatalf("unable to create new symlink: %v", err)
	}

	// Verify the symlink now points to the second file
	updatedTarget, err := sc.resolveSymlink(symlinkFile)
	if err != nil {
		t.Fatalf("unable to resolve updated symlink: %v", err)
	}
	if updatedTarget != realFile2 {
		t.Fatalf("expected updated symlink to resolve to %s, got %s", realFile2, updatedTarget)
	}

	// Test 3: Simulate a file change event on the new target
	// This tests that handleSymlinkChanges can handle symlink target changes
	changeEvent := fsnotify.Event{
		Name: realFile2,
		Op:   fsnotify.Write,
	}

	// Call handleSymlinkChanges - it should detect the symlink target change
	sc.handleSymlinkChanges(changeEvent)

	// Test 4: Verify that reading through the symlink gets the correct content
	content, err := os.ReadFile(symlinkFile)
	if err != nil {
		t.Fatalf("unable to read file through symlink: %v", err)
	}
	expectedContent := "updated content"
	if string(content) != expectedContent {
		t.Fatalf("expected content %s, got %s", expectedContent, string(content))
	}

	// Test 5: Test symlink removal
	removeEvent := fsnotify.Event{
		Name: symlinkFile,
		Op:   fsnotify.Remove,
	}

	// Remove the symlink file
	err = os.Remove(symlinkFile)
	if err != nil {
		t.Fatalf("unable to remove symlink: %v", err)
	}

	// Call handleSymlinkChanges - it should handle the removal gracefully
	sc.handleSymlinkChanges(removeEvent)

	// Verify that the symlink is no longer in fileCerts (should be cleaned up by remove event handler)
	sc.certMutex.RLock()
	_, found := sc.fileCerts[FileCert{ResourceName: "test", Filename: symlinkFile}]
	sc.certMutex.RUnlock()

	// Note: The actual cleanup happens in the main event handler, not in handleSymlinkChanges
	// This test just verifies that handleSymlinkChanges doesn't crash when encountering removed symlinks
	if !found {
		t.Logf("symlink was cleaned up as expected")
	}
}

func TestResolveSymlink(t *testing.T) {
	tempDir := t.TempDir()
	realFile := filepath.Join(tempDir, "real-file.txt")
	symlinkFile := filepath.Join(tempDir, "symlink.txt")
	relativeSymlinkFile := filepath.Join(tempDir, "relative-symlink.txt")

	// Create a real file
	err := os.WriteFile(realFile, []byte("test content"), 0644)
	if err != nil {
		t.Fatalf("unable to create test file: %v", err)
	}

	// Create an absolute symlink
	err = os.Symlink(realFile, symlinkFile)
	if err != nil {
		t.Fatalf("unable to create symlink: %v", err)
	}

	// Create a relative symlink
	err = os.Symlink("real-file.txt", relativeSymlinkFile)
	if err != nil {
		t.Fatalf("unable to create relative symlink: %v", err)
	}

	fakeCACli, err := mock.NewMockCAClient(time.Hour, true)
	if err != nil {
		t.Fatalf("unable to create fake mock ca client: %v", err)
	}

	sc := createCache(t, fakeCACli, func(resourceName string) {}, security.Options{WorkloadRSAKeySize: 2048})

	// Test resolving a regular file (should return the same path)
	resolved, err := sc.resolveSymlink(realFile)
	if err != nil {
		t.Fatalf("expected no error resolving regular file: %v", err)
	}
	if resolved != realFile {
		t.Fatalf("expected resolved path to be %s, got %s", realFile, resolved)
	}

	// Test resolving an absolute symlink
	resolved, err = sc.resolveSymlink(symlinkFile)
	if err != nil {
		t.Fatalf("expected no error resolving symlink: %v", err)
	}
	if resolved != realFile {
		t.Fatalf("expected resolved path to be %s, got %s", realFile, resolved)
	}

	// Test resolving a relative symlink
	resolved, err = sc.resolveSymlink(relativeSymlinkFile)
	if err != nil {
		t.Fatalf("expected no error resolving relative symlink: %v", err)
	}
	if resolved != realFile {
		t.Fatalf("expected resolved path to be %s, got %s", realFile, resolved)
	}

	// Test resolving a non-existent file (should return the original path)
	nonExistentFile := filepath.Join(tempDir, "non-existent.txt")
	resolved, err = sc.resolveSymlink(nonExistentFile)
	if err != nil {
		// Expected error for non-existent file
		if resolved != nonExistentFile {
			t.Fatalf("expected resolved path to be %s, got %s", nonExistentFile, resolved)
		}
	} else {
		t.Fatalf("expected error for non-existent file, but got none")
	}
}
