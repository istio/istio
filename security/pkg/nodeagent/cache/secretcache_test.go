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
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"istio.io/istio/pkg/file"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/testcerts"
	"istio.io/istio/security/pkg/nodeagent/caclient/providers/mock"
	"istio.io/istio/security/pkg/nodeagent/cafile"
	pkiutil "istio.io/istio/security/pkg/pki/util"
	"istio.io/pkg/log"
)

func TestWorkloadAgentGenerateSecret(t *testing.T) {
	t.Run("plugin", func(t *testing.T) {
		testWorkloadAgentGenerateSecret(t, true)
	})
	t.Run("no plugin", func(t *testing.T) {
		testWorkloadAgentGenerateSecret(t, false)
	})
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

func testWorkloadAgentGenerateSecret(t *testing.T, isUsingPluginProvider bool) {
	fakeCACli, err := mock.NewMockCAClient(time.Hour, true)
	var got, want []byte
	if err != nil {
		t.Fatalf("Error creating Mock CA client: %v", err)
	}
	opt := &security.Options{}

	if isUsingPluginProvider {
		fakePlugin := mock.NewMockTokenExchangeServer(nil)
		opt.TokenExchanger = fakePlugin
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

func TestRotateTime(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name        string
		created     time.Time
		expire      time.Time
		gracePeriod float64
		expected    time.Duration
	}{
		{
			name:        "already expired",
			created:     now.Add(-time.Second * 2),
			expire:      now.Add(-time.Second),
			gracePeriod: 0.5,
			expected:    0,
		},
		{
			name:        "grace period .50",
			created:     now,
			expire:      now.Add(time.Hour),
			gracePeriod: 0.5,
			expected:    time.Minute * 30,
		},
		{
			name:        "grace period .25",
			created:     now,
			expire:      now.Add(time.Hour),
			gracePeriod: 0.25,
			expected:    time.Minute * 45,
		},
		{
			name:        "grace period .75",
			created:     now,
			expire:      now.Add(time.Hour),
			gracePeriod: 0.75,
			expected:    time.Minute * 15,
		},
		{
			name:        "grace period 1",
			created:     now,
			expire:      now.Add(time.Hour),
			gracePeriod: 1,
			expected:    0,
		},
		{
			name:        "grace period 0",
			created:     now,
			expire:      now.Add(time.Hour),
			gracePeriod: 0,
			expected:    time.Hour,
		},
		{
			name:        "grace period .25 shifted",
			created:     now.Add(time.Minute * 30),
			expire:      now.Add(time.Minute * 90),
			gracePeriod: 0.25,
			expected:    time.Minute * 75,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			sc := &SecretManagerClient{configOptions: &security.Options{SecretRotationGracePeriodRatio: tt.gracePeriod}}
			got := sc.rotateTime(security.SecretItem{CreatedTime: tt.created, ExpireTime: tt.expire})
			if !almostEqual(got, tt.expected) {
				t.Fatalf("expected %v got %v", tt.expected, got)
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
		t.Fatalf("expected to get error")
	}

	if gotSecret != nil {
		t.Fatalf("Expected to get nil secret but got %v", gotSecret)
	}

	rootResource := "file-root:" + rootCertPath
	gotSecretRoot, err := sc.GenerateSecret(rootResource)

	if err == nil {
		t.Fatalf("Expected to get error, but did not get")
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
		if !bytes.Equal(expectedSecret.RootCert, gotSecret.RootCert) {
			t.Fatalf("root cert: expected %v but got %v", expectedSecret.RootCert,
				gotSecret.RootCert)
		}
	} else {
		if !bytes.Equal(expectedSecret.CertificateChain, gotSecret.CertificateChain) {
			t.Fatalf("cert chain: expected %s but got %s", string(expectedSecret.CertificateChain),
				string(gotSecret.CertificateChain))
		}
		if !bytes.Equal(expectedSecret.PrivateKey, gotSecret.PrivateKey) {
			t.Fatalf("private key: expected %s but got %s", string(expectedSecret.PrivateKey), string(gotSecret.PrivateKey))
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
	u.Expect(map[string]int{security.RootCertReqResourceName: 1})
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
		t.Fatalf("deduplicate test failed!")
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

func TestOSCACertGenerateSecret(t *testing.T) {
	fakeCACli, err := mock.NewMockCAClient(time.Hour, false)
	if err != nil {
		t.Fatalf("Error creating Mock CA client: %v", err)
	}
	opt := &security.Options{}

	fakePlugin := mock.NewMockTokenExchangeServer(nil)
	opt.TokenExchanger = fakePlugin

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
	opt := &security.Options{}

	fakePlugin := mock.NewMockTokenExchangeServer(nil)
	opt.TokenExchanger = fakePlugin

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
