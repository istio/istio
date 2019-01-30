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
	"fmt"
	"io/ioutil"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"istio.io/istio/security/pkg/nodeagent/model"
	"istio.io/istio/security/pkg/nodeagent/secretfetcher"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var (
	mockCertChain1st    = []string{"foo", "rootcert"}
	mockCertChainRemain = []string{"bar", "rootcert"}
	testResourceName    = "default"

	k8sKey               = []byte("fake private k8sKey")
	k8sCertChain         = []byte("fake cert chain")
	k8sGenericSecretName = "test-generic-scrt"
	k8sTestGenericSecret = &v1.Secret{
		Data: map[string][]byte{
			"cert": k8sCertChain,
			"key":  k8sKey,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sGenericSecretName,
			Namespace: "test-namespace",
		},
		Type: "test-generic-secret",
	}
	k8sTlsSecretName = "test-tls-scrt"
	k8sTestTlsSecret = &v1.Secret{
		Data: map[string][]byte{
			"tls.crt":  k8sCertChain,
			"tls.key":  k8sKey,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sTlsSecretName,
			Namespace: "test-namespace",
		},
		Type: "test-tls-secret",
	}
)

func TestWorkloadAgentGenerateSecret(t *testing.T) {
	fakeCACli := newMockCAClient()
	opt := Options{
		SecretTTL:        time.Minute,
		RotationInterval: 300 * time.Microsecond,
		EvictionDuration: 2 * time.Second,
	}
	fetcher := &secretfetcher.SecretFetcher{
		UseCaClient: true,
		CaClient:    fakeCACli,
	}
	sc := NewSecretCache(fetcher, notifyCb, opt)
	atomic.StoreUint32(&sc.skipTokenExpireCheck, 0)
	defer func() {
		sc.Close()
		atomic.StoreUint32(&sc.skipTokenExpireCheck, 1)
	}()

	if got, want := opt.AlwaysValidTokenFlag, false; got != want {
		t.Errorf("opt.AlwaysValidTokenFlag default: got: %v, want: %v", got, want)
	}

	proxyID := "proxy1-id"
	ctx := context.Background()
	gotSecret, err := sc.GenerateSecret(ctx, proxyID, testResourceName, "jwtToken1")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}

	if got, want := gotSecret.CertificateChain, convertToBytes(mockCertChain1st); bytes.Compare(got, want) != 0 {
		t.Errorf("CertificateChain: got: %v, want: %v", got, want)
	}

	if got, want := sc.SecretExist(proxyID, testResourceName, "jwtToken1", gotSecret.Version), true; got != want {
		t.Errorf("SecretExist: got: %v, want: %v", got, want)
	}
	if got, want := sc.SecretExist(proxyID, testResourceName, "nonexisttoken", gotSecret.Version), false; got != want {
		t.Errorf("SecretExist: got: %v, want: %v", got, want)
	}

	gotSecretRoot, err := sc.GenerateSecret(ctx, proxyID, RootCertReqResourceName, "jwtToken1")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	if got, want := gotSecretRoot.RootCert, []byte("rootcert"); bytes.Compare(got, want) != 0 {
		t.Errorf("CertificateChain: got: %v, want: %v", got, want)
	}

	if got, want := sc.SecretExist(proxyID, RootCertReqResourceName, "jwtToken1", gotSecretRoot.Version), true; got != want {
		t.Errorf("SecretExist: got: %v, want: %v", got, want)
	}
	if got, want := sc.SecretExist(proxyID, RootCertReqResourceName, "nonexisttoken", gotSecretRoot.Version), false; got != want {
		t.Errorf("SecretExist: got: %v, want: %v", got, want)
	}

	key := ConnKey{
		ProxyID:      proxyID,
		ResourceName: testResourceName,
	}
	cachedSecret, found := sc.secrets.Load(key)
	if !found {
		t.Errorf("Failed to find secret for proxy %q from secret store: %v", proxyID, err)
	}
	if !reflect.DeepEqual(*gotSecret, cachedSecret) {
		t.Errorf("Secret key: got %+v, want %+v", *gotSecret, cachedSecret)
	}

	// Try to get secret again using different jwt token, verify secret is re-generated.
	gotSecret, err = sc.GenerateSecret(ctx, proxyID, testResourceName, "newToken")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	if got, want := gotSecret.CertificateChain, convertToBytes(mockCertChainRemain); bytes.Compare(got, want) != 0 {
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

func TestWorkloadAgentRefreshSecret(t *testing.T) {
	fakeCACli := newMockCAClient()
	opt := Options{
		SecretTTL:        200 * time.Microsecond,
		RotationInterval: 200 * time.Microsecond,
		EvictionDuration: 10 * time.Second,
	}
	fetcher := &secretfetcher.SecretFetcher{
		UseCaClient: true,
		CaClient:    fakeCACli,
	}
	sc := NewSecretCache(fetcher, notifyCb, opt)
	atomic.StoreUint32(&sc.skipTokenExpireCheck, 0)
	defer func() {
		sc.Close()
		atomic.StoreUint32(&sc.skipTokenExpireCheck, 1)
	}()

	testProxyID := "proxy1-id"
	_, err := sc.GenerateSecret(context.Background(), testProxyID, testResourceName, "jwtToken1")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
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
		ProxyID:      testProxyID,
		ResourceName: testResourceName,
	}
	if _, found := sc.secrets.Load(key); !found {
		t.Errorf("Failed to find secret for %+v from cache", key)
	}

	sc.DeleteSecret(testProxyID, testResourceName)
	if _, found := sc.secrets.Load(key); found {
		t.Errorf("Found deleted secret for %+v from cache", key)
	}
}

// TestGatewayAgentGenerateSecret verifies that ingress gateway agent manages secret cache correctly.
func TestGatewayAgentGenerateSecret(t *testing.T) {
	sc := createSecretCache()
	fetcher := sc.fetcher
	atomic.StoreUint32(&sc.skipTokenExpireCheck, 0)
	defer func() {
		sc.Close()
		atomic.StoreUint32(&sc.skipTokenExpireCheck, 1)
	}()

	fetcher.AddSecret(k8sTestGenericSecret)
	proxyID := "proxy1-id"
	ctx := context.Background()
	gotSecret, err := sc.GenerateSecret(ctx, proxyID, k8sGenericSecretName, "")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	expectedGenericSecret := &model.SecretItem{
		ResourceName:     k8sGenericSecretName,
		CertificateChain: k8sCertChain,
		PrivateKey:       k8sKey,
	}
	if err := verifySecret(gotSecret, expectedGenericSecret); err != nil {
		t.Errorf("Secret verification failed: %v", err)
	}

	if got, want := sc.SecretExist(proxyID, k8sGenericSecretName, "", gotSecret.Version), true; got != want {
		t.Errorf("SecretExist: got: %v, want: %v", got, want)
	}
	if got, want := sc.SecretExist(proxyID, "nonexistsecret", "", gotSecret.Version), false; got != want {
		t.Errorf("SecretExist: got: %v, want: %v", got, want)
	}

	key := ConnKey{
		ProxyID:      proxyID,
		ResourceName: k8sGenericSecretName,
	}
	cachedSecret, found := sc.secrets.Load(key)
	if !found {
		t.Errorf("Failed to find secret for proxy %q from secret store: %v", proxyID, err)
	}
	if !reflect.DeepEqual(*gotSecret, cachedSecret) {
		t.Errorf("Secret key: got %+v, want %+v", *gotSecret, cachedSecret)
	}

	// Try to get secret again, verify the same secret is returned.
	gotSecret, err = sc.GenerateSecret(ctx, proxyID, k8sGenericSecretName, "")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	if err := verifySecret(gotSecret, expectedGenericSecret); err != nil {
		t.Errorf("Secret verification failed: %v", err)
	}

	if _, err := sc.GenerateSecret(ctx, proxyID, "nonexistk8ssecret", ""); err == nil {
		t.Error("Generating secret using a non existing kubernetes secret should fail")
	}

	// Add a Tls secret and verify that secret can be read and cached.
	fetcher.AddSecret(k8sTestTlsSecret)
	proxyID = "proxy2-id"
	gotSecret, err = sc.GenerateSecret(ctx, proxyID, k8sTlsSecretName, "")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	expectedTlsSecret := &model.SecretItem{
		ResourceName:     k8sTlsSecretName,
		CertificateChain: k8sCertChain,
		PrivateKey:       k8sKey,
	}
	if err := verifySecret(gotSecret, expectedTlsSecret); err != nil {
		t.Errorf("Secret verification failed: %v", err)
	}

	if got, want := sc.SecretExist(proxyID, k8sTlsSecretName, "", gotSecret.Version), true; got != want {
		t.Errorf("SecretExist: got: %v, want: %v", got, want)
	}
	key = ConnKey{
		ProxyID:      proxyID,
		ResourceName: k8sTlsSecretName,
	}
	cachedSecret, found = sc.secrets.Load(key)
	if !found {
		t.Errorf("Failed to find secret for proxy %q from secret store: %v", proxyID, err)
	}
	if !reflect.DeepEqual(*gotSecret, cachedSecret) {
		t.Errorf("Secret key: got %+v, want %+v", *gotSecret, cachedSecret)
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

func createSecretCache() *SecretCache {
	fetcher := &secretfetcher.SecretFetcher{
		UseCaClient: false,
	}
	fetcher.Init(fake.NewSimpleClientset().CoreV1())
	ch := make(chan struct{})
	fetcher.Run(ch)
	opt := Options{
		SecretTTL:        time.Minute,
		RotationInterval: 300 * time.Microsecond,
		EvictionDuration: 2 * time.Second,
	}
	return NewSecretCache(fetcher, notifyCb, opt)
}

// TestGatewayAgentDeleteSecret verifies that ingress gateway agent deletes secret cache correctly.
func TestGatewayAgentDeleteSecret(t *testing.T) {
	sc := createSecretCache()
	fetcher := sc.fetcher
	atomic.StoreUint32(&sc.skipTokenExpireCheck, 0)
	defer func() {
		sc.Close()
		atomic.StoreUint32(&sc.skipTokenExpireCheck, 1)
	}()

	fetcher.AddSecret(k8sTestGenericSecret)
	fetcher.AddSecret(k8sTestTlsSecret)
	proxyID := "proxy1-id"
	ctx := context.Background()
	gotSecret, err := sc.GenerateSecret(ctx, proxyID, k8sGenericSecretName, "")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	if got, want := sc.SecretExist(proxyID, k8sGenericSecretName, "", gotSecret.Version), true; got != want {
		t.Errorf("SecretExist: got: %v, want: %v", got, want)
	}
	gotSecret, err = sc.GenerateSecret(ctx, proxyID, k8sTlsSecretName, "")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	if got, want := sc.SecretExist(proxyID, k8sTlsSecretName, "", gotSecret.Version), true; got != want {
		t.Errorf("SecretExist: got: %v, want: %v", got, want)
	}

	sc.DeleteK8sSecret(k8sGenericSecretName)
	sc.DeleteK8sSecret(k8sTlsSecretName)
	if got, want := sc.SecretExist(proxyID, k8sGenericSecretName, "", gotSecret.Version), false; got != want {
		t.Errorf("SecretExist: got: %v, want: %v", got, want)
	}
	if got, want := sc.SecretExist(proxyID, k8sTlsSecretName, "", gotSecret.Version), false; got != want {
		t.Errorf("SecretExist: got: %v, want: %v", got, want)
	}
}

// TestGatewayAgentUpdateSecret verifies that ingress gateway agent updates secret cache correctly.
func TestGatewayAgentUpdateSecret(t *testing.T) {
	sc := createSecretCache()
	fetcher := sc.fetcher
	atomic.StoreUint32(&sc.skipTokenExpireCheck, 0)
	defer func() {
		sc.Close()
		atomic.StoreUint32(&sc.skipTokenExpireCheck, 1)
	}()

	fetcher.AddSecret(k8sTestGenericSecret)
	proxyID := "proxy1-id"
	ctx := context.Background()
	gotSecret, err := sc.GenerateSecret(ctx, proxyID, k8sGenericSecretName, "")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	if got, want := sc.SecretExist(proxyID, k8sGenericSecretName, "", gotSecret.Version), true; got != want {
		t.Errorf("SecretExist: got: %v, want: %v", got, want)
	}
	newTime := gotSecret.CreatedTime.Add(time.Duration(10) * time.Second)
	newK8sTestSecret := model.SecretItem{
		CertificateChain: []byte("new cert chain"),
		PrivateKey:       []byte("new private key"),
		ResourceName:     k8sGenericSecretName,
		Token:            gotSecret.Token,
		CreatedTime:      newTime,
		Version:          newTime.String(),
	}
	sc.UpdateK8sSecret(k8sGenericSecretName, newK8sTestSecret)
	if got, want := sc.SecretExist(proxyID, k8sGenericSecretName, "", gotSecret.Version), false; got != want {
		t.Errorf("SecretExist: got: %v, want: %v", got, want)
	}
}

func TestConstructCSRHostName(t *testing.T) {
	data, err := ioutil.ReadFile("./testdata/testjwt")
	if err != nil {
		t.Errorf("failed to read test jwt file %v", err)
	}
	testJwt := string(data)

	cases := []struct {
		trustDomain string
		token       string
		expected    string
		errFlag     bool
	}{
		{
			token:    testJwt,
			expected: "spiffe://cluster.local/ns/default/sa/sleep",
			errFlag:  false,
		},
		{
			trustDomain: "fooDomain",
			token:       testJwt,
			expected:    "spiffe://fooDomain/ns/default/sa/sleep",
			errFlag:     false,
		},
		{
			token:    "faketoken",
			expected: "",
			errFlag:  true,
		},
	}
	for _, c := range cases {
		got, err := constructCSRHostName(c.trustDomain, c.token)
		if err != nil {
			if c.errFlag == false {
				t.Errorf("constructCSRHostName no error, but got %v", err)
			}
			continue
		}

		if err == nil && c.errFlag == true {
			t.Error("constructCSRHostName error")
		}

		if got != c.expected {
			t.Errorf("constructCSRHostName got %q, want %q", got, c.expected)
		}
	}
}

func TestSetAlwaysValidTokenFlag(t *testing.T) {
	fakeCACli := newMockCAClient()
	opt := Options{
		SecretTTL:            200 * time.Microsecond,
		RotationInterval:     200 * time.Microsecond,
		EvictionDuration:     10 * time.Second,
		AlwaysValidTokenFlag: true,
	}
	fetcher := &secretfetcher.SecretFetcher{
		UseCaClient: true,
		CaClient:    fakeCACli,
	}
	sc := NewSecretCache(fetcher, notifyCb, opt)
	defer func() {
		sc.Close()
	}()

	if got, want := sc.isTokenExpired(), false; got != want {
		t.Errorf("isTokenExpired: got: %v, want: %v", got, want)
	}
	_, err := sc.GenerateSecret(context.Background(), "proxy1-id", testResourceName, "jwtToken1")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
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
}

func verifySecret(gotSecret *model.SecretItem, expectedSecret *model.SecretItem) error {
	if expectedSecret.ResourceName != gotSecret.ResourceName {
		return fmt.Errorf("resource name verification error: expected %s but got %s", expectedSecret.ResourceName,
			gotSecret.ResourceName)
	}
	if !bytes.Equal(expectedSecret.CertificateChain, gotSecret.CertificateChain) {
		return fmt.Errorf("cert chain verification error: expected %v but got %v", expectedSecret.CertificateChain,
			gotSecret.CertificateChain)
	}
	if !bytes.Equal(expectedSecret.PrivateKey, gotSecret.PrivateKey) {
		return fmt.Errorf("k8sKey verification error: expected %v but got %v", expectedSecret.PrivateKey,
			gotSecret.PrivateKey)
	}
	return nil
}

func notifyCb(string, string, *model.SecretItem) error {
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
	certValidTTLInSec int64) ([]string /*PEM-encoded certificate chain*/, error) {
	if atomic.LoadUint64(&c.signInvokeCount) == 0 {
		atomic.AddUint64(&c.signInvokeCount, 1)
		return nil, status.Error(codes.Internal, "some internal error")
	}

	if atomic.LoadUint64(&c.signInvokeCount) == 1 {
		atomic.AddUint64(&c.signInvokeCount, 1)
		return mockCertChain1st, nil
	}

	return mockCertChainRemain, nil
}

func convertToBytes(ss []string) []byte {
	res := []byte{}
	for _, s := range ss {
		res = append(res, []byte(s)...)
	}
	return res
}
