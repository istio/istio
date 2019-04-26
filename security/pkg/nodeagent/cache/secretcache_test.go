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
	certchain, _        = ioutil.ReadFile("./testdata/cert-chain.pem")
	mockCertChain1st    = []string{"foo", "rootcert"}
	mockCertChainRemain = []string{string(certchain)}
	testResourceName    = "default"

	k8sKey               = []byte("fake private k8sKey")
	k8sCertChain         = []byte("fake cert chain")
	k8sCaCert            = []byte("fake ca cert")
	k8sGenericSecretName = "test-generic-scrt"
	k8sTestGenericSecret = &v1.Secret{
		Data: map[string][]byte{
			"cert":   k8sCertChain,
			"key":    k8sKey,
			"cacert": k8sCaCert,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sGenericSecretName,
			Namespace: "test-namespace",
		},
		Type: "test-generic-secret",
	}
	k8sTLSSecretName = "test-tls-scrt"
	k8sTestTLSSecret = &v1.Secret{
		Data: map[string][]byte{
			"tls.crt": k8sCertChain,
			"tls.key": k8sKey,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sTLSSecretName,
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
		InitialBackoff:   10,
		SkipValidateCert: true,
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

	checkBool(t, "opt.AlwaysValidTokenFlag default", opt.AlwaysValidTokenFlag, false)

	conID := "proxy1-id"
	ctx := context.Background()
	gotSecret, err := sc.GenerateSecret(ctx, conID, testResourceName, "jwtToken1")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}

	if got, want := gotSecret.CertificateChain, convertToBytes(mockCertChain1st); !bytes.Equal(got, want) {
		t.Errorf("CertificateChain: got: %v, want: %v", got, want)
	}

	checkBool(t, "SecretExist", sc.SecretExist(conID, testResourceName, "jwtToken1", gotSecret.Version), true)
	checkBool(t, "SecretExist", sc.SecretExist(conID, testResourceName, "nonexisttoken", gotSecret.Version), false)

	gotSecretRoot, err := sc.GenerateSecret(ctx, conID, RootCertReqResourceName, "jwtToken1")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	if got, want := gotSecretRoot.RootCert, []byte("rootcert"); !bytes.Equal(got, want) {
		t.Errorf("CertificateChain: got: %v, want: %v", got, want)
	}

	checkBool(t, "SecretExist", sc.SecretExist(conID, RootCertReqResourceName, "jwtToken1", gotSecretRoot.Version), true)
	checkBool(t, "SecretExist", sc.SecretExist(conID, RootCertReqResourceName, "nonexisttoken", gotSecretRoot.Version), false)

	if got, want := atomic.LoadUint64(&sc.rootCertChangedCount), uint64(0); got != want {
		t.Errorf("rootCertChangedCount: got: %v, want: %v", got, want)
	}

	key := ConnKey{
		ConnectionID: conID,
		ResourceName: testResourceName,
	}
	cachedSecret, found := sc.secrets.Load(key)
	if !found {
		t.Errorf("Failed to find secret for proxy %q from secret store: %v", conID, err)
	}
	if !reflect.DeepEqual(*gotSecret, cachedSecret) {
		t.Errorf("Secret key: got %+v, want %+v", *gotSecret, cachedSecret)
	}

	sc.configOptions.SkipValidateCert = false
	// Try to get secret again using different jwt token, verify secret is re-generated.
	gotSecret, err = sc.GenerateSecret(ctx, conID, testResourceName, "newToken")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	if got, want := gotSecret.CertificateChain, convertToBytes(mockCertChainRemain); !bytes.Equal(got, want) {
		t.Errorf("CertificateChain: got: %v, want: %v", got, want)
	}

	// Root cert is parsed from CSR response, it's updated since 2nd CSR is different from 1st.
	if got, want := atomic.LoadUint64(&sc.rootCertChangedCount), uint64(1); got != want {
		t.Errorf("rootCertChangedCount: got: %v, want: %v", got, want)
	}

	// Wait until unused secrets are evicted.
	wait := 500 * time.Millisecond
	retries := 0
	for ; retries < 3; retries++ {
		time.Sleep(wait)
		if _, found := sc.secrets.Load(conID); found {
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
		InitialBackoff:   10,
		SkipValidateCert: true,
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

	testConnID := "proxy1-id"
	_, err := sc.GenerateSecret(context.Background(), testConnID, testResourceName, "jwtToken1")
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
		ConnectionID: testConnID,
		ResourceName: testResourceName,
	}
	if _, found := sc.secrets.Load(key); !found {
		t.Errorf("Failed to find secret for %+v from cache", key)
	}

	sc.DeleteSecret(testConnID, testResourceName)
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

	connID1 := "proxy1-id"
	connID2 := "proxy2-id"
	ctx := context.Background()

	type expectedSecret struct {
		exist  bool
		secret *model.SecretItem
	}

	cases := []struct {
		addSecret       *v1.Secret
		connID          string
		expectedSecrets []expectedSecret
	}{
		{
			addSecret: k8sTestGenericSecret,
			connID:    connID1,
			expectedSecrets: []expectedSecret{
				{
					exist: true,
					secret: &model.SecretItem{
						ResourceName:     k8sGenericSecretName,
						CertificateChain: k8sCertChain,
						PrivateKey:       k8sKey,
					},
				},
				{
					exist: true,
					secret: &model.SecretItem{
						ResourceName: k8sGenericSecretName + "-cacert",
						RootCert:     k8sCaCert,
					},
				},
			},
		},
		{
			addSecret: k8sTestTLSSecret,
			connID:    connID2,
			expectedSecrets: []expectedSecret{
				{
					exist: true,
					secret: &model.SecretItem{
						ResourceName:     k8sTLSSecretName,
						CertificateChain: k8sCertChain,
						PrivateKey:       k8sKey,
					},
				},
				{
					exist: false,
					secret: &model.SecretItem{
						ResourceName: k8sTLSSecretName + "-cacert",
					},
				},
			},
		},
	}

	for _, c := range cases {
		fetcher.AddSecret(c.addSecret)
		for _, es := range c.expectedSecrets {
			gotSecret, err := sc.GenerateSecret(ctx, c.connID, es.secret.ResourceName, "")
			if es.exist {
				if err != nil {
					t.Fatalf("Failed to get secrets: %v", err)
				}
				if err := verifySecret(gotSecret, es.secret); err != nil {
					t.Errorf("Secret verification failed: %v", err)
				}
				checkBool(t, "SecretExist", sc.SecretExist(c.connID, es.secret.ResourceName, "", gotSecret.Version), true)
				checkBool(t, "SecretExist", sc.SecretExist(c.connID, "nonexistsecret", "", gotSecret.Version), false)
			}
			key := ConnKey{
				ConnectionID: c.connID,
				ResourceName: es.secret.ResourceName,
			}
			cachedSecret, found := sc.secrets.Load(key)
			if es.exist {
				if !found {
					t.Errorf("Failed to find secret for proxy %q from secret store: %v", c.connID, err)
				}
				if !reflect.DeepEqual(*gotSecret, cachedSecret) {
					t.Errorf("Secret key: got %+v, want %+v", *gotSecret, cachedSecret)
				}
			}
			if _, err := sc.GenerateSecret(ctx, c.connID, "nonexistk8ssecret", ""); err == nil {
				t.Error("Generating secret using a non existing kubernetes secret should fail")
			}
		}
	}

	// Wait until unused secrets are evicted.
	wait := 500 * time.Millisecond
	retries := 0
	for ; retries < 3; retries++ {
		time.Sleep(wait)
		if _, found := sc.secrets.Load(connID1); found {
			// Retry after some sleep.
			wait *= 2
			continue
		}
		if _, found := sc.secrets.Load(connID2); found {
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
	fetcher.InitWithKubeClient(fake.NewSimpleClientset().CoreV1())
	ch := make(chan struct{})
	fetcher.Run(ch)
	opt := Options{
		SecretTTL:        time.Minute,
		RotationInterval: 300 * time.Microsecond,
		EvictionDuration: 2 * time.Second,
		InitialBackoff:   10,
		SkipValidateCert: true,
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
	fetcher.AddSecret(k8sTestTLSSecret)
	connID := "proxy1-id"
	ctx := context.Background()
	gotSecret, err := sc.GenerateSecret(ctx, connID, k8sGenericSecretName, "")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sGenericSecretName, "", gotSecret.Version), true)

	gotSecret, err = sc.GenerateSecret(ctx, connID, k8sGenericSecretName+"-cacert", "")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sGenericSecretName+"-cacert", "", gotSecret.Version), true)

	gotSecret, err = sc.GenerateSecret(ctx, connID, k8sTLSSecretName, "")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sTLSSecretName, "", gotSecret.Version), true)

	_, err = sc.GenerateSecret(ctx, connID, k8sTLSSecretName+"-cacert", "")
	if err == nil {
		t.Fatalf("Get unexpected secrets: %v", err)
	}
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sTLSSecretName+"-cacert", "", gotSecret.Version), false)

	sc.DeleteK8sSecret(k8sGenericSecretName)
	sc.DeleteK8sSecret(k8sGenericSecretName + "-cacert")
	sc.DeleteK8sSecret(k8sTLSSecretName)
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sGenericSecretName, "", gotSecret.Version), false)
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sGenericSecretName+"-cacert", "", gotSecret.Version), false)
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sTLSSecretName, "", gotSecret.Version), false)
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
	connID := "proxy1-id"
	ctx := context.Background()
	gotSecret, err := sc.GenerateSecret(ctx, connID, k8sGenericSecretName, "")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sGenericSecretName, "", gotSecret.Version), true)
	gotSecret, err = sc.GenerateSecret(ctx, connID, k8sGenericSecretName+"-cacert", "")
	if err != nil {
		t.Fatalf("Failed to get secrets: %v", err)
	}
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sGenericSecretName+"-cacert", "", gotSecret.Version), true)
	newTime := gotSecret.CreatedTime.Add(time.Duration(10) * time.Second)
	newK8sTestSecret := model.SecretItem{
		CertificateChain: []byte("new cert chain"),
		PrivateKey:       []byte("new private key"),
		RootCert:         []byte("new root cert"),
		ResourceName:     k8sGenericSecretName,
		Token:            gotSecret.Token,
		CreatedTime:      newTime,
		Version:          newTime.String(),
	}
	sc.UpdateK8sSecret(k8sGenericSecretName, newK8sTestSecret)
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sGenericSecretName, "", gotSecret.Version), false)
	sc.UpdateK8sSecret(k8sGenericSecretName+"-cacert", newK8sTestSecret)
	checkBool(t, "SecretExist", sc.SecretExist(connID, k8sGenericSecretName+"-cacert", "", gotSecret.Version), false)
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

		if c.errFlag == true {
			t.Error("constructCSRHostName error")
		}

		if got != c.expected {
			t.Errorf("constructCSRHostName got %q, want %q", got, c.expected)
		}
	}
}

func checkBool(t *testing.T, name string, got bool, want bool) {
	if got != want {
		t.Errorf("%s: got: %v, want: %v", name, got, want)
	}
}

func TestSetAlwaysValidTokenFlag(t *testing.T) {
	fakeCACli := newMockCAClient()
	opt := Options{
		SecretTTL:            200 * time.Microsecond,
		RotationInterval:     200 * time.Microsecond,
		EvictionDuration:     10 * time.Second,
		InitialBackoff:       10,
		AlwaysValidTokenFlag: true,
		SkipValidateCert:     true,
	}
	fetcher := &secretfetcher.SecretFetcher{
		UseCaClient: true,
		CaClient:    fakeCACli,
	}
	sc := NewSecretCache(fetcher, notifyCb, opt)
	defer func() {
		sc.Close()
	}()

	checkBool(t, "isTokenExpired", sc.isTokenExpired(), false)
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

func notifyCb(_ string, _ string, _ *model.SecretItem) error {
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
