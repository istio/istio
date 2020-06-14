// Copyright Istio Authors. All Rights Reserved.
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

package spiffe

import (
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spiffe/spire/test/spiretest"
	"github.com/stretchr/testify/require"
)

var (
	validResponse = `
{
	"spiffe_sequence": 1,
	"spiffe_refresh_hint": 450000,
	"keys": [
		{
		"kty": "RSA",
		"use": "x509-svid",
		"n": "r10W2IcjT-vvSTpaFsS4OAcPOX87kw-zKZuJgXhxDhkOQyBdPZpUfK4H8yZ2q14Laym4bmiMLocIeGP70kUXcp9T4SP-P0DmBTPx3hVgP3YteHzaKsja056VtDs9kAufmFGemTSCenMt7aSlryUbLRO0H-__fTeNkCXR7uIoqRfU6jL0nN4EBh02q724iGuX6dpJcQam5bEJjq6Kn4Ry4qn1xHXqQXM4o2f6xDT13sp4U32stpmKh0HOd1WWKr0WRYnAh4GnToKr21QySZi9QWTea3zqeFmti-Isji1dKZkgZA2S89BdTWSLe6S_9lV0mtdXvDaT8RmaIX72jE_AbhnbUYV84pNYv-T2LtIKoi5PjWk0raaYoexAjtCWiu3PnizxjYOnNwpzgQN9Qh_rY2jv74cgzG50_Ft1B7XUiakNFxAiD1k6pNuiu4toY0Es7qt1yeqaC2zcIuuV7HUv1AbFBkIdF5quJHVtZ5AE1MCh1ipLPq-lIjmFdQKSRdbssVw8yq9FtFVyVqTz9GnQtoctCIPGQqmJDWmt8E7gjFhweUQo-fGgGuTlZRl9fiPQ6luPyGQ1WL6wH79G9eu4UtmgUDNwq7kpYq0_NQ5vw_1WQSY3LsPclfKzkZ-Lw2RVef-SFVVvUFMcd_3ALeeEnnSe4GSY-7vduPUAE5qMH7M",
		"e": "AQAB",
		"x5c": ["MIIGlDCCBHygAwIBAgIQEW25APa7S9Sj/Nj6V6GxQTANBgkqhkiG9w0BAQsFADCBwTELMAkGA1UEBhMCVVMxEzARBgNVBAgTCkNhbGlmb3JuaWExFjAUBgNVBAcTDU1vdW50YWluIFZpZXcxEzARBgNVBAoTCkdvb2dsZSBMTEMxDjAMBgNVBAsTBUNsb3VkMWAwXgYDVQQDDFdpc3Rpb192MV9jbG91ZF93b3JrbG9hZF9yb290LXNpZ25lci0wLTIwMTgtMDQtMjVUMTQ6MTE6MzMtMDc6MDAgSzoxLCAxOkg1MnZnd0VtM3RjOjA6MTgwIBcNMTgwNDI1MjExMTMzWhgPMjExODA0MjUyMjExMzNaMIHBMQswCQYDVQQGEwJVUzETMBEGA1UECBMKQ2FsaWZvcm5pYTEWMBQGA1UEBxMNTW91bnRhaW4gVmlldzETMBEGA1UEChMKR29vZ2xlIExMQzEOMAwGA1UECxMFQ2xvdWQxYDBeBgNVBAMMV2lzdGlvX3YxX2Nsb3VkX3dvcmtsb2FkX3Jvb3Qtc2lnbmVyLTAtMjAxOC0wNC0yNVQxNDoxMTozMy0wNzowMCBLOjEsIDE6SDUydmd3RW0zdGM6MDoxODCCAiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBAK9dFtiHI0/r70k6WhbEuDgHDzl/O5MPsymbiYF4cQ4ZDkMgXT2aVHyuB/MmdqteC2spuG5ojC6HCHhj+9JFF3KfU+Ej/j9A5gUz8d4VYD92LXh82irI2tOelbQ7PZALn5hRnpk0gnpzLe2kpa8lGy0TtB/v/303jZAl0e7iKKkX1Ooy9JzeBAYdNqu9uIhrl+naSXEGpuWxCY6uip+EcuKp9cR16kFzOKNn+sQ09d7KeFN9rLaZiodBzndVliq9FkWJwIeBp06Cq9tUMkmYvUFk3mt86nhZrYviLI4tXSmZIGQNkvPQXU1ki3ukv/ZVdJrXV7w2k/EZmiF+9oxPwG4Z21GFfOKTWL/k9i7SCqIuT41pNK2mmKHsQI7Qlortz54s8Y2DpzcKc4EDfUIf62No7++HIMxudPxbdQe11ImpDRcQIg9ZOqTboruLaGNBLO6rdcnqmgts3CLrlex1L9QGxQZCHReariR1bWeQBNTAodYqSz6vpSI5hXUCkkXW7LFcPMqvRbRVclak8/Rp0LaHLQiDxkKpiQ1prfBO4IxYcHlEKPnxoBrk5WUZfX4j0Opbj8hkNVi+sB+/RvXruFLZoFAzcKu5KWKtPzUOb8P9VkEmNy7D3JXys5Gfi8NkVXn/khVVb1BTHHf9wC3nhJ50nuBkmPu73bj1ABOajB+zAgMBAAGjgYMwgYAwDgYDVR0PAQH/BAQDAgEGMB0GA1UdJQQWMBQGCCsGAQUFBwMBBggrBgEFBQcDAjAPBgNVHRMBAf8EBTADAQH/MB0GA1UdDgQWBBQ/VsuyjgRDAEmcZjyJ77619Js9ijAfBgNVHSMEGDAWgBQ/VsuyjgRDAEmcZjyJ77619Js9ijANBgkqhkiG9w0BAQsFAAOCAgEAUc5QJOqxmMJY0E2rcHEWQYRah1vat3wuIHtEZ3SkSumyj+y9eyIHb9XTTyc4SyGyX1n8Rary8oSgQV4cbyJTFXEEQOGLHB9/98EKThgJtfPsos2WKe/59S8yN05onpxcaL9y4S295Kv9kcSQxLm5UfjlqsKeHJZymvxiYzmBox7LA1zqcLYZvslJNkJxKAk5JA66iyDSQqOK7jIixn8pi305dFGCZglUFStwWqY6Rc9rR8EycVhSx2AhrvT7OQTVdKLfoKA84D8JZJPB7hrxqKf7JJFs87Kjt7c/5bXPFJ2osmjoNYnbHjiq64bh20sSCd630qvhhePLwjjOlBPiFyK36o/hQN871AEm1SCHy+aQcfJqF5KTgPnZQy5D+D/CGau+BfkO+WCGDVxRleYBJ4g2NbATolygB2KWXrj07U/WaWqV2hERbkmxXFh6cUdlkX2MeoG4v6ZD2OKAPx5DpJCfp0TEq6PznP+Z1mLd/ZjGsOF8R2WGQJEuU8HRzvsr0wsX9UyLMqf5XViDK11V/W+dcIvjHCayBpX2se3dfex5jFht+JcQc+iwB8caSXkR6tGSiargEtSJODORacO9IB8b6W8Sm//JWf/8zyiCcMm1i2yVVphwE1kczFwunAh0JB896VaXGVxXeKEAMQoXHjgDdCYp8/Etxjb8UkCmyjU="]
		}
	]
}`
)

func TestGenSpiffeURI(t *testing.T) {
	oldTrustDomain := GetTrustDomain()
	defer SetTrustDomain(oldTrustDomain)

	testCases := []struct {
		namespace      string
		trustDomain    string
		serviceAccount string
		expectedError  string
		expectedURI    string
	}{
		{
			serviceAccount: "sa",
			trustDomain:    defaultTrustDomain,
			expectedError:  "namespace or service account empty for SPIFFE uri",
		},
		{
			namespace:     "ns",
			trustDomain:   defaultTrustDomain,
			expectedError: "namespace or service account empty for SPIFFE uri",
		},
		{
			namespace:      "namespace-foo",
			serviceAccount: "service-bar",
			trustDomain:    defaultTrustDomain,
			expectedURI:    "spiffe://cluster.local/ns/namespace-foo/sa/service-bar",
		},
		{
			namespace:      "foo",
			serviceAccount: "bar",
			trustDomain:    defaultTrustDomain,
			expectedURI:    "spiffe://cluster.local/ns/foo/sa/bar",
		},
		{
			namespace:      "foo",
			serviceAccount: "bar",
			trustDomain:    "kube-federating-id@testproj.iam.gserviceaccount.com",
			expectedURI:    "spiffe://kube-federating-id.testproj.iam.gserviceaccount.com/ns/foo/sa/bar",
		},
	}
	for id, tc := range testCases {
		SetTrustDomain(tc.trustDomain)
		got, err := GenSpiffeURI(tc.namespace, tc.serviceAccount)
		if tc.expectedError == "" && err != nil {
			t.Errorf("teste case [%v] failed, error %v", id, tc)
		}
		if tc.expectedError != "" {
			if err == nil {
				t.Errorf("want get error %v, got nil", tc.expectedError)
			} else if !strings.Contains(err.Error(), tc.expectedError) {
				t.Errorf("want error contains %v,  got error %v", tc.expectedError, err)
			}
			continue
		}
		if got != tc.expectedURI {
			t.Errorf("unexpected subject name, want %v, got %v", tc.expectedURI, got)
		}
	}
}

func TestGetSetTrustDomain(t *testing.T) {
	oldTrustDomain := GetTrustDomain()
	defer SetTrustDomain(oldTrustDomain)

	cases := []struct {
		in  string
		out string
	}{
		{
			in:  "test.local",
			out: "test.local",
		},
		{
			in:  "test@local",
			out: "test.local",
		},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			SetTrustDomain(c.in)
			if GetTrustDomain() != c.out {
				t.Errorf("expected=%s, actual=%s", c.out, GetTrustDomain())
			}
		})
	}
}

func TestMustGenSpiffeURI(t *testing.T) {
	if nonsense := MustGenSpiffeURI("", ""); nonsense != "spiffe://cluster.local/ns//sa/" {
		t.Errorf("Unexpected spiffe URI for empty namespace and service account: %s", nonsense)
	}
}

func TestGenCustomSpiffe(t *testing.T) {
	oldTrustDomain := GetTrustDomain()
	defer SetTrustDomain(oldTrustDomain)

	testCases := []struct {
		trustDomain string
		identity    string
		expectedURI string
	}{
		{
			identity:    "foo",
			trustDomain: "mesh.com",
			expectedURI: "spiffe://mesh.com/foo",
		},
		{
			//identity is empty
			trustDomain: "mesh.com",
			expectedURI: "",
		},
	}
	for id, tc := range testCases {
		SetTrustDomain(tc.trustDomain)
		got := GenCustomSpiffe(tc.identity)

		if got != tc.expectedURI {
			t.Errorf("Test id: %v , unexpected subject name, want %v, got %v", id, tc.expectedURI, got)
		}
	}
}

func TestRetrieveSpiffeBundleRootCerts(t *testing.T) {
	testCases := []struct {
		name        string
		certDNSName string
		trustCert   bool
		status      int
		body        string
		errContains string
	}{
		{
			name:        "success",
			certDNSName: "localhost",
			trustCert:   true,
			status:      http.StatusOK,
			body:        validResponse,
		},
		{
			name:        "Unauthenticated cert",
			certDNSName: "localhost",
			trustCert:   false,
			status:      http.StatusOK,
			body:        validResponse,
			errContains: "x509: certificate signed by unknown authority",
		},
		{
			name:        "Cert DNS name unmatch",
			certDNSName: "mismatch",
			trustCert:   true,
			status:      http.StatusOK,
			body:        validResponse,
			errContains: "x509: certificate is valid for mismatch, not localhost",
		},
		{
			name:        "non-200 status",
			certDNSName: "localhost",
			trustCert:   true,
			status:      http.StatusServiceUnavailable,
			body:        "tHe SYsTEm iS DowN",
			errContains: "unexpected status 503 fetching bundle: tHe SYsTEm iS DowN",
		},
		{
			name:        "invalid bundle content",
			certDNSName: "localhost",
			trustCert:   true,
			status:      http.StatusOK,
			body:        "NOT JSON",
			errContains: "failed to decode bundle",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			serverCert, serverKey := createServerCertificate(t, testCase.certDNSName)

			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(testCase.status)
				_, _ = w.Write([]byte(testCase.body))
			}))
			server.TLS = &tls.Config{
				Certificates: []tls.Certificate{
					{
						Certificate: [][]byte{serverCert.Raw},
						PrivateKey:  serverKey,
					},
				},
			}
			server.StartTLS()
			defer server.Close()

			_, port, err := net.SplitHostPort(server.Listener.Addr().String())
			if err != nil {
				t.Fatalf("Failed to split the SPIFFE endpoint host port: %v", err)
			}

			trustedCerts := []*x509.Certificate{}
			if testCase.trustCert {
				trustedCerts = append(trustedCerts, serverCert)
			}
			rootCerts, err := RetrieveSpiffeBundleRootCerts("https://localhost:"+port, trustedCerts)
			if testCase.errContains != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), testCase.errContains)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, rootCerts)
		})
	}
}

func createServerCertificate(t *testing.T, dnsName string) (*x509.Certificate, crypto.Signer) {
	return spiretest.SelfSignCertificate(t, &x509.Certificate{
		SerialNumber: big.NewInt(0),
		DNSNames:     []string{dnsName},
		NotAfter:     time.Now().Add(time.Hour),
	})
}
