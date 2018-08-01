// Copyright 2018 Istio Authors
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
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"istio.io/istio/security/pkg/nodeagent/model"
)

var (
	mockCertificateChain1st    = []byte{01}
	mockCertificateChainRemain = []byte{02}

	fakeSpiffeID = "spiffe://cluster.local/ns/bar/sa/foo"
)

func TestGetSecret(t *testing.T) {
	skipTokenExpireCheck = false
	fakeCACli := newMockCAClient()
	opt := Options{
		SecretTTL:        time.Minute,
		RotationInterval: 300 * time.Microsecond,
		EvictionDuration: 2 * time.Second,
	}
	sc := NewSecretCache(fakeCACli, notifyCb, opt)
	defer func() {
		sc.Close()
		skipTokenExpireCheck = true
	}()

	proxyID := "proxy1-id"
	ctx := context.Background()
	gotSecret, err := sc.GetSecret(ctx, proxyID, fakeSpiffeID, "jwtToken1")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	if got, want := gotSecret.CertificateChain, mockCertificateChain1st; bytes.Compare(got, want) != 0 {
		t.Errorf("CertificateChain: got: %v, want: %v", got, want)
	}

	if got, want := sc.SecretExist(proxyID, fakeSpiffeID, "jwtToken1", gotSecret.Version), true; got != want {
		t.Errorf("SecretExist: got: %v, want: %v", got, want)
	}
	if got, want := sc.SecretExist(proxyID, fakeSpiffeID, "nonexisttoken", gotSecret.Version), false; got != want {
		t.Errorf("SecretExist: got: %v, want: %v", got, want)
	}

	cachedSecret, found := sc.secrets.Load(proxyID)
	if !found {
		t.Errorf("Failed to find secret for proxy %q from secret store: %v", proxyID, err)
	}
	if !reflect.DeepEqual(*gotSecret, cachedSecret) {
		t.Errorf("Secret key: got %+v, want %+v", *gotSecret, cachedSecret)
	}

	// Try to get secret again using different jwt token, verify secret is re-generated.
	gotSecret, err = sc.GetSecret(ctx, proxyID, fakeSpiffeID, "newToken")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	if got, want := gotSecret.CertificateChain, mockCertificateChainRemain; bytes.Compare(got, want) != 0 {
		t.Errorf("CertificateChain: got: %v, want: %v", got, want)
	}

	// Wait until unused secrets are evicted.
	wait := 500 * time.Millisecond
	retries := 0
	for ; retries < 3; retries++ {
		time.Sleep(wait)
		if _, found := sc.secrets.Load(proxyID); found {
			// Retry after some sleep.
			wait *= 2
			continue
		}

		break
	}
	if retries == 3 {
		t.Errorf("Unused secrets failed to be evicted from cache")
	}
}

func TestRefreshSecret(t *testing.T) {
	fakeCACli := newMockCAClient()
	skipTokenExpireCheck = false
	opt := Options{
		SecretTTL:        300 * time.Microsecond,
		RotationInterval: 300 * time.Microsecond,
		EvictionDuration: 10 * time.Second,
	}
	sc := NewSecretCache(fakeCACli, notifyCb, opt)
	defer func() {
		sc.Close()
		skipTokenExpireCheck = true
	}()

	_, err := sc.GetSecret(context.Background(), "proxy1-id", fakeSpiffeID, "jwtToken1")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}

	// Wait until key rotation job run to update cached secret.
	wait := 400 * time.Millisecond
	retries := 0
	for ; retries < 3; retries++ {
		time.Sleep(wait)
		if atomic.LoadUint64(&sc.secretChangedCount) == uint64(0) {
			// Retry after some sleep.
			wait *= 2
			continue
		}

		break
	}
	if retries == 3 {
		t.Errorf("Cached secret failed to get refreshed, %d", atomic.LoadUint64(&sc.secretChangedCount))
	}
}

func notifyCb(string, *model.SecretItem) error {
	return nil
}

type mockCAClient struct {
	signInvokeCount uint64
}

func newMockCAClient() *mockCAClient {
	cl := mockCAClient{}
	atomic.StoreUint64(&cl.signInvokeCount, 0)
	return &cl
}

func (c *mockCAClient) CSRSign(ctx context.Context, csrPEM []byte, subjectID string,
	certValidTTLInSec int64) ([]byte /*PEM-encoded certificate chain*/, error) {
	atomic.AddUint64(&c.signInvokeCount, 1)

	if atomic.LoadUint64(&c.signInvokeCount) == 1 {
		return mockCertificateChain1st, nil
	}

	return mockCertificateChainRemain, nil
}
