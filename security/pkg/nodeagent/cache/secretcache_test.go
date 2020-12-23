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
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/nodeagent/cache/mock"
	"istio.io/istio/security/pkg/nodeagent/secretfetcher"
	nodeagentutil "istio.io/istio/security/pkg/nodeagent/util"
	"istio.io/pkg/filewatcher"
)

func TestWorkloadAgentGenerateSecretWithoutPluginProvider(t *testing.T) {
	testWorkloadAgentGenerateSecret(t, false)
}

func TestWorkloadAgentGenerateSecretWithPluginProvider(t *testing.T) {
	testWorkloadAgentGenerateSecret(t, true)
}

func testWorkloadAgentGenerateSecret(t *testing.T, isUsingPluginProvider bool) {
	// The mocked CA client returns 2 errors before returning a valid response.
	fakeCACli, err := mock.NewMockCAClient(2, time.Hour)
	if err != nil {
		t.Fatalf("Error creating Mock CA client: %v", err)
	}
	opt := &security.Options{
		RotationInterval: 100 * time.Millisecond,
		EvictionDuration: 0,
	}

	if isUsingPluginProvider {
		// The mocked token exchanger server returns 3 errors before returning a valid response.
		fakePlugin := mock.NewMockTokenExchangeServer(3)
		opt.TokenExchangers = []security.TokenExchanger{fakePlugin}
	}

	fetcher := &secretfetcher.SecretFetcher{
		CaClient: fakeCACli,
	}
	sc := NewSecretCache(fetcher, notifyCb, opt)
	defer func() {
		sc.Close()
	}()

	conID := "proxy1-id"
	ctx := context.Background()
	gotSecret, err := sc.GenerateSecret(ctx, conID, WorkloadKeyCertResourceName, "jwtToken1")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}

	if got, want := gotSecret.CertificateChain, []byte(strings.Join(fakeCACli.GeneratedCerts[0], "")); !bytes.Equal(got, want) {
		t.Errorf("Got unexpected certificate chain #1. Got: %v, want: %v", string(got), string(want))
	}

	checkBool(t, "SecretExist", sc.SecretExist(conID, WorkloadKeyCertResourceName, "jwtToken1", gotSecret.Version), true)
	checkBool(t, "SecretExist", sc.SecretExist(conID, WorkloadKeyCertResourceName, "nonexisttoken", gotSecret.Version), false)

	gotSecretRoot, err := sc.GenerateSecret(ctx, conID, RootCertReqResourceName, "jwtToken1")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	// Root cert is the last element in the generated certs.
	if got, want := gotSecretRoot.RootCert, []byte(fakeCACli.GeneratedCerts[0][2]); !bytes.Equal(got, want) {
		t.Errorf("Got unexpected root certificate. Got: %v\n want: %v", string(got), string(want))
	}

	checkBool(t, "SecretExist", sc.SecretExist(conID, RootCertReqResourceName, "jwtToken1", gotSecretRoot.Version), true)
	checkBool(t, "SecretExist", sc.SecretExist(conID, RootCertReqResourceName, "nonexisttoken", gotSecretRoot.Version), false)

	if got, want := atomic.LoadUint64(&sc.rootCertChangedCount), uint64(0); got != want {
		t.Errorf("Got unexpected rootCertChangedCount: Got: %v\n want: %v", got, want)
	}

	// Try to get secret again using different jwt token, verify secret is re-generated.
	gotSecret, err = sc.GenerateSecret(ctx, conID, WorkloadKeyCertResourceName, "newToken")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}

	if got, want := gotSecret.CertificateChain, []byte(strings.Join(fakeCACli.GeneratedCerts[1], "")); !bytes.Equal(got, want) {
		t.Errorf("Got unexpected certificate chain #2. Got: %v, want: %v", string(got), string(want))
	}

	// Root cert stays the same.
	if got, want := atomic.LoadUint64(&sc.rootCertChangedCount), uint64(0); got != want {
		t.Errorf("Got unexpected rootCertChangedCount: Got: %v\n want: %v", got, want)
	}
}

func TestWorkloadAgentRefreshSecret(t *testing.T) {
	fakeCACli, err := mock.NewMockCAClient(0, time.Millisecond)
	if err != nil {
		t.Fatalf("Error creating Mock CA client: %v", err)
	}
	opt := &security.Options{
		RotationInterval: 100 * time.Millisecond,
		EvictionDuration: 0,
	}
	fetcher := &secretfetcher.SecretFetcher{
		CaClient: fakeCACli,
	}
	sc := NewSecretCache(fetcher, notifyCb, opt)
	defer func() {
		sc.Close()
	}()

	testConnID := "proxy1-id"
	_, err = sc.GenerateSecret(context.Background(), testConnID, WorkloadKeyCertResourceName, "jwtToken1")
	if err != nil {
		t.Fatalf("Failed to get secrets for %q: %v", testConnID, err)
	}

	// Wait until key rotation job run to update cached secret.
	wait := 200 * time.Millisecond
	retries := 0
	for ; retries < 5; retries++ {
		time.Sleep(wait)
		if atomic.LoadUint64(&sc.secretChangedCount) == uint64(0) {
			// Retry after some sleep.
			wait *= 2
			continue
		}

		break
	}
	if retries == 5 {
		t.Errorf("Cached secret failed to get refreshed, %d", atomic.LoadUint64(&sc.secretChangedCount))
	}

	key := ConnKey{
		ConnectionID: testConnID,
		ResourceName: WorkloadKeyCertResourceName,
	}
	if _, found := sc.secrets.Load(key); !found {
		t.Errorf("Failed to find secret for %+v from cache", key)
	}

	sc.DeleteSecret(testConnID, WorkloadKeyCertResourceName)
	if _, found := sc.secrets.Load(key); found {
		t.Errorf("Found deleted secret for %+v from cache", key)
	}
}

func createSecretCache() *SecretCache {
	fetcher := &secretfetcher.SecretFetcher{}
	opt := &security.Options{
		RotationInterval: 100 * time.Millisecond,
		EvictionDuration: 0,
	}
	return NewSecretCache(fetcher, notifyCb, opt)
}

// nolint: unparam
func checkBool(t *testing.T, name string, got bool, want bool) {
	if got != want {
		t.Errorf("%s: got: %v, want: %v", name, got, want)
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

	sc := createSecretCache()
	defer sc.Close()
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

	sc := createSecretCache()
	defer sc.Close()
	for _, tc := range testCases {
		ret := sc.keyCertificateExist(tc.certPath, tc.keyPath)
		if tc.expectResult != ret {
			t.Errorf("unexpected result is returned!")
		}
	}
}

func notifyCb(_ ConnKey, _ *security.SecretItem) error {
	return nil
}

// TestWorkloadAgentGenerateSecretFromFile tests generating secrets from existing files on a
// secretcache instance.
func TestWorkloadAgentGenerateSecretFromFile(t *testing.T) {
	fakeCACli, err := mock.NewMockCAClient(0, time.Hour)
	if err != nil {
		t.Fatalf("Error creating Mock CA client: %v", err)
	}
	opt := &security.Options{
		// Large rotation, to make sure the test is not
		// affected - this is not a rotation test.
		RotationInterval: 2 * time.Hour,
		EvictionDuration: 0,
		UseTokenForCSR:   true,
	}

	fetcher := &secretfetcher.SecretFetcher{
		CaClient: fakeCACli,
	}

	var wgAddedWatch sync.WaitGroup
	var notifyEvent sync.WaitGroup

	addedWatchProbe := func(_ string, _ bool) { wgAddedWatch.Done() }

	var closed bool
	notifyCallback := func(_ ConnKey, _ *security.SecretItem) error {
		if !closed {
			notifyEvent.Done()
		}
		return nil
	}

	// Supply a fake watcher so that we can watch file events.
	var fakeWatcher *filewatcher.FakeWatcher
	newFileWatcher, fakeWatcher = filewatcher.NewFakeWatcher(addedWatchProbe)

	sc := NewSecretCache(fetcher, notifyCallback, opt)
	defer func() {
		closed = true
		sc.Close()
		newFileWatcher = filewatcher.NewWatcher
	}()

	rootCertPath := "./testdata/root-cert.pem"
	keyPath := "./testdata/key.pem"
	certChainPath := "./testdata/cert-chain.pem"
	sc.existingRootCertFile = rootCertPath
	sc.existingKeyFile = keyPath
	sc.existingCertChainFile = certChainPath
	certchain, err := ioutil.ReadFile(certChainPath)
	if err != nil {
		t.Fatalf("Error reading the cert chain file: %v", err)
	}
	privateKey, err := ioutil.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("Error reading the private key file: %v", err)
	}
	rootCert, err := ioutil.ReadFile(rootCertPath)
	if err != nil {
		t.Fatalf("Error reading the root cert file: %v", err)
	}

	conID := "proxy1-id"
	ctx := context.Background()

	wgAddedWatch.Add(1) // Watch should be added for cert file.
	// notify is called only on rotation - it was accidentally called
	// because rotation interval was set to a very small value.
	gotSecret, err := sc.GenerateSecret(ctx, conID, WorkloadKeyCertResourceName, "jwtToken1")

	wgAddedWatch.Wait()

	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	checkBool(t, "SecretExist", sc.SecretExist(conID, WorkloadKeyCertResourceName, "jwtToken1", gotSecret.Version), true)
	expectedSecret := &security.SecretItem{
		ResourceName:     WorkloadKeyCertResourceName,
		CertificateChain: certchain,
		PrivateKey:       privateKey,
	}
	if err := verifySecret(gotSecret, expectedSecret); err != nil {
		t.Errorf("Secret verification failed: %v", err)
	}

	wgAddedWatch.Add(1) // Watch should be added for root file.

	// This test was passing because it overrides secret rotation to 100ms,
	// and the callback happens to be called on rotation (by accident I think, since
	// secret is not supposed to be rotated - probably a sideffect of the token?)
	gotSecretRoot, err := sc.GenerateSecret(ctx, conID, RootCertReqResourceName, "jwtToken1")

	wgAddedWatch.Wait()

	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	checkBool(t, "SecretExist", sc.SecretExist(conID, RootCertReqResourceName, "jwtToken1", gotSecretRoot.Version), true)
	if got, want := atomic.LoadUint64(&sc.rootCertChangedCount), uint64(0); got != want {
		t.Errorf("rootCertChangedCount: got: %v, want: %v", got, want)
	}

	rootExpiration, err := nodeagentutil.ParseCertAndGetExpiryTimestamp(rootCert)
	if err != nil {
		t.Fatalf("Failed to get the expiration time from the existing root file")
	}
	expectedSecret = &security.SecretItem{
		ResourceName: RootCertReqResourceName,
		RootCert:     rootCert,
		ExpireTime:   rootExpiration,
	}
	if err := verifyRootCASecret(gotSecretRoot, expectedSecret); err != nil {
		t.Errorf("Secret verification failed: %v", err)
	}

	key := ConnKey{
		ConnectionID: conID,
		ResourceName: WorkloadKeyCertResourceName,
	}
	cachedSecret, found := sc.secrets.Load(key)
	if !found {
		t.Errorf("Failed to find secret for proxy %q from secret store: %v", conID, err)
	}
	if !reflect.DeepEqual(*gotSecret, cachedSecret) {
		t.Errorf("Secret key: got %+v, want %+v", *gotSecret, cachedSecret)
	}

	// Inject a file write event and validate that Notify is called.
	notifyEvent.Add(1)
	fakeWatcher.InjectEvent(certChainPath, fsnotify.Event{
		Name: certChainPath,
		Op:   fsnotify.Write,
	})
	// Test will timeout after a long time if rotation doesn't happen.
	// A channel with timeout would work better.
	notifyEvent.Wait()
}

// TestWorkloadAgentGenerateSecretFromFileOverSds tests generating secrets from existing files on a
// secretcache instance, specified over SDS.
func TestWorkloadAgentGenerateSecretFromFileOverSds(t *testing.T) {
	fetcher := &secretfetcher.SecretFetcher{}

	opt := &security.Options{
		RotationInterval: 200 * time.Millisecond,
		EvictionDuration: 0,
	}

	var wgAddedWatch sync.WaitGroup
	var notifyEvent sync.WaitGroup
	var closed bool

	addedWatchProbe := func(_ string, _ bool) { wgAddedWatch.Done() }

	notifyCallback := func(_ ConnKey, _ *security.SecretItem) error {
		if !closed {
			notifyEvent.Done()
		}
		return nil
	}

	// Supply a fake watcher so that we can watch file events.
	var fakeWatcher *filewatcher.FakeWatcher
	newFileWatcher, fakeWatcher = filewatcher.NewFakeWatcher(addedWatchProbe)

	sc := NewSecretCache(fetcher, notifyCallback, opt)
	defer func() {
		closed = true
		sc.Close()
		newFileWatcher = filewatcher.NewWatcher
	}()
	rootCertPath, _ := filepath.Abs("./testdata/root-cert.pem")
	keyPath, _ := filepath.Abs("./testdata/key.pem")
	certChainPath, _ := filepath.Abs("./testdata/cert-chain.pem")
	certchain, err := ioutil.ReadFile(certChainPath)
	if err != nil {
		t.Fatalf("Error reading the cert chain file: %v", err)
	}
	privateKey, err := ioutil.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("Error reading the private key file: %v", err)
	}
	rootCert, err := ioutil.ReadFile(rootCertPath)
	if err != nil {
		t.Fatalf("Error reading the root cert file: %v", err)
	}

	resource := fmt.Sprintf("file-cert:%s~%s", certChainPath, keyPath)
	conID := "proxy1-id"
	ctx := context.Background()

	// Since we do not have rotation enabled, we do not get secret notification.
	wgAddedWatch.Add(1) // Watch should be added for cert file.

	gotSecret, err := sc.GenerateSecret(ctx, conID, resource, "jwtToken1")

	wgAddedWatch.Wait()

	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	checkBool(t, "SecretExist", sc.SecretExist(conID, resource, "jwtToken1", gotSecret.Version), true)
	expectedSecret := &security.SecretItem{
		ResourceName:     resource,
		CertificateChain: certchain,
		PrivateKey:       privateKey,
	}
	if err := verifySecret(gotSecret, expectedSecret); err != nil {
		t.Errorf("Secret verification failed: %v", err)
	}

	rootResource := "file-root:" + rootCertPath

	wgAddedWatch.Add(1) // Watch should be added for root file.

	gotSecretRoot, err := sc.GenerateSecret(ctx, conID, rootResource, "jwtToken1")

	wgAddedWatch.Wait()

	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	checkBool(t, "SecretExist", sc.SecretExist(conID, rootResource, "jwtToken1", gotSecretRoot.Version), true)
	if got, want := atomic.LoadUint64(&sc.rootCertChangedCount), uint64(0); got != want {
		t.Errorf("rootCertChangedCount: got: %v, want: %v", got, want)
	}

	rootExpiration, err := nodeagentutil.ParseCertAndGetExpiryTimestamp(rootCert)
	if err != nil {
		t.Fatalf("Failed to get the expiration time from the existing root file")
	}
	expectedSecret = &security.SecretItem{
		ResourceName: rootResource,
		RootCert:     rootCert,
		ExpireTime:   rootExpiration,
	}
	if err := verifyRootCASecret(gotSecretRoot, expectedSecret); err != nil {
		t.Errorf("Secret verification failed: %v", err)
	}

	key := ConnKey{
		ConnectionID: conID,
		ResourceName: resource,
	}
	cachedSecret, found := sc.secrets.Load(key)
	if !found {
		t.Errorf("Failed to find secret for proxy %q from secret store: %v", conID, err)
	}
	if !reflect.DeepEqual(*gotSecret, cachedSecret) {
		t.Errorf("Secret key: got %+v, want %+v", *gotSecret, cachedSecret)
	}

	// Inject a file write event and validate that Notify is called.
	notifyEvent.Add(1)
	fakeWatcher.InjectEvent(certChainPath, fsnotify.Event{
		Name: certChainPath,
		Op:   fsnotify.Write,
	})
	notifyEvent.Wait()
}

func TestWorkloadAgentGenerateSecretFromFileOverSdsWithBogusFiles(t *testing.T) {
	fetcher := &secretfetcher.SecretFetcher{}
	originalTimeout := totalTimeout
	totalTimeout = time.Second * 1
	defer func() {
		totalTimeout = originalTimeout
	}()

	opt := &security.Options{
		RotationInterval: 1 * time.Millisecond,
		EvictionDuration: 0,
	}
	sc := NewSecretCache(fetcher, notifyCb, opt)
	defer func() {
		sc.Close()
	}()
	rootCertPath, _ := filepath.Abs("./testdata/root-cert-bogus.pem")
	keyPath, _ := filepath.Abs("./testdata/key-bogus.pem")
	certChainPath, _ := filepath.Abs("./testdata/cert-chain-bogus.pem")

	resource := fmt.Sprintf("file-cert:%s~%s", certChainPath, keyPath)
	conID := "proxy1-id"
	ctx := context.Background()

	gotSecret, err := sc.GenerateSecret(ctx, conID, resource, "jwtToken1")

	if err == nil {
		t.Fatalf("expected to get error")
	}

	if gotSecret != nil {
		t.Fatalf("Expected to get nil secret but got %v", gotSecret)
	}

	rootResource := "file-root:" + rootCertPath
	gotSecretRoot, err := sc.GenerateSecret(ctx, conID, rootResource, "jwtToken1")

	if err == nil {
		t.Fatalf("Expected to get error, but did not get")
	}
	if !strings.Contains(err.Error(), "no such file or directory") {
		t.Fatalf("Expected file not found error, but got %v", err)
	}
	if gotSecretRoot != nil {
		t.Fatalf("Expected to get nil secret but got %v", gotSecret)
	}
	length := 0
	sc.secrets.Range(func(k interface{}, v interface{}) bool {
		length++
		return true
	})
	if length > 0 {
		t.Fatalf("Expected zero secrets in cache, but got %v", length)
	}
}

func verifySecret(gotSecret *security.SecretItem, expectedSecret *security.SecretItem) error {
	if expectedSecret.ResourceName != gotSecret.ResourceName {
		return fmt.Errorf("resource name verification error: expected %s but got %s", expectedSecret.ResourceName,
			gotSecret.ResourceName)
	}
	if !bytes.Equal(expectedSecret.CertificateChain, gotSecret.CertificateChain) {
		return fmt.Errorf("cert chain verification error: expected %s but got %s", string(expectedSecret.CertificateChain),
			string(gotSecret.CertificateChain))
	}
	if !bytes.Equal(expectedSecret.PrivateKey, gotSecret.PrivateKey) {
		return fmt.Errorf("k8sKey verification error: expected %s but got %s", string(expectedSecret.PrivateKey),
			string(gotSecret.PrivateKey))
	}
	return nil
}

func verifyRootCASecret(gotSecret *security.SecretItem, expectedSecret *security.SecretItem) error {
	if expectedSecret.ResourceName != gotSecret.ResourceName {
		return fmt.Errorf("resource name verification error: expected %s but got %s", expectedSecret.ResourceName,
			gotSecret.ResourceName)
	}
	if !bytes.Equal(expectedSecret.RootCert, gotSecret.RootCert) {
		return fmt.Errorf("root cert verification error: expected %v but got %v", expectedSecret.RootCert,
			gotSecret.RootCert)
	}
	if expectedSecret.ExpireTime != gotSecret.ExpireTime {
		return fmt.Errorf("root cert expiration time verification error: expected %v but got %v",
			expectedSecret.ExpireTime, gotSecret.ExpireTime)
	}
	return nil
}

func TestShouldRotate(t *testing.T) {
	now := time.Now()

	testCases := map[string]struct {
		secret       *security.SecretItem
		sc           *SecretCache
		shouldRotate bool
	}{
		"Not in grace period": {
			secret: &security.SecretItem{
				ResourceName: "test1",
				ExpireTime:   now.Add(time.Hour),
				CreatedTime:  now.Add(-time.Hour),
			},
			sc:           &SecretCache{configOptions: &security.Options{SecretRotationGracePeriodRatio: 0.4}},
			shouldRotate: false,
		},
		"In grace period": {
			secret: &security.SecretItem{
				ResourceName: "test2",
				ExpireTime:   now.Add(time.Hour),
				CreatedTime:  now.Add(-time.Hour),
			},
			sc:           &SecretCache{configOptions: &security.Options{SecretRotationGracePeriodRatio: 0.6}},
			shouldRotate: true,
		},
		"Passed the expiration": {
			secret: &security.SecretItem{
				ResourceName: "test3",
				ExpireTime:   now.Add(-time.Minute),
				CreatedTime:  now.Add(-time.Hour),
			},
			sc:           &SecretCache{configOptions: &security.Options{SecretRotationGracePeriodRatio: 0}},
			shouldRotate: true,
		},
	}

	for name, tc := range testCases {
		if tc.sc.shouldRotate(tc.secret) != tc.shouldRotate {
			t.Errorf("%s: unexpected shouldRotate return. Expected: %v", name, tc.shouldRotate)
		}
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
