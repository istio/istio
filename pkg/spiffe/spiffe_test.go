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
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/util/sets"
)

var (
	validSpiffeX509Bundle = `
{
	"spiffe_sequence": 1,
	"spiffe_refresh_hint": 450000,
	"keys": [
		{
		"kty": "RSA",
		"use": "x509-svid",
		"n": "r10W2IcjT-vvSTpaFsS4OAcPOX87kw-zKZuJgXhxDhkOQyBdPZpUfK4H8yZ2q14Laym4bmiMLocIeGP70k` +
		`UXcp9T4SP-P0DmBTPx3hVgP3YteHzaKsja056VtDs9kAufmFGemTSCenMt7aSlryUbLRO0H-__fTeNkCXR7uIoq` +
		`RfU6jL0nN4EBh02q724iGuX6dpJcQam5bEJjq6Kn4Ry4qn1xHXqQXM4o2f6xDT13sp4U32stpmKh0HOd1WWKr0W` +
		`RYnAh4GnToKr21QySZi9QWTea3zqeFmti-Isji1dKZkgZA2S89BdTWSLe6S_9lV0mtdXvDaT8RmaIX72jE_Abhn` +
		`bUYV84pNYv-T2LtIKoi5PjWk0raaYoexAjtCWiu3PnizxjYOnNwpzgQN9Qh_rY2jv74cgzG50_Ft1B7XUiakNFx` +
		`AiD1k6pNuiu4toY0Es7qt1yeqaC2zcIuuV7HUv1AbFBkIdF5quJHVtZ5AE1MCh1ipLPq-lIjmFdQKSRdbssVw8y` +
		`q9FtFVyVqTz9GnQtoctCIPGQqmJDWmt8E7gjFhweUQo-fGgGuTlZRl9fiPQ6luPyGQ1WL6wH79G9eu4UtmgUDNw` +
		`q7kpYq0_NQ5vw_1WQSY3LsPclfKzkZ-Lw2RVef-SFVVvUFMcd_3ALeeEnnSe4GSY-7vduPUAE5qMH7M",
		"e": "AQAB",
		"x5c": ["MIIGlDCCBHygAwIBAgIQEW25APa7S9Sj/Nj6V6GxQTANBgkqhkiG9w0BAQsFADCBwTELMAkGA1UEBhM` +
		`CVVMxEzARBgNVBAgTCkNhbGlmb3JuaWExFjAUBgNVBAcTDU1vdW50YWluIFZpZXcxEzARBgNVBAoTCkdvb2dsZS` +
		`BMTEMxDjAMBgNVBAsTBUNsb3VkMWAwXgYDVQQDDFdpc3Rpb192MV9jbG91ZF93b3JrbG9hZF9yb290LXNpZ25lc` +
		`i0wLTIwMTgtMDQtMjVUMTQ6MTE6MzMtMDc6MDAgSzoxLCAxOkg1MnZnd0VtM3RjOjA6MTgwIBcNMTgwNDI1MjEx` +
		`MTMzWhgPMjExODA0MjUyMjExMzNaMIHBMQswCQYDVQQGEwJVUzETMBEGA1UECBMKQ2FsaWZvcm5pYTEWMBQGA1U` +
		`EBxMNTW91bnRhaW4gVmlldzETMBEGA1UEChMKR29vZ2xlIExMQzEOMAwGA1UECxMFQ2xvdWQxYDBeBgNVBAMMV2` +
		`lzdGlvX3YxX2Nsb3VkX3dvcmtsb2FkX3Jvb3Qtc2lnbmVyLTAtMjAxOC0wNC0yNVQxNDoxMTozMy0wNzowMCBLO` +
		`jEsIDE6SDUydmd3RW0zdGM6MDoxODCCAiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBAK9dFtiHI0/r70k6` +
		`WhbEuDgHDzl/O5MPsymbiYF4cQ4ZDkMgXT2aVHyuB/MmdqteC2spuG5ojC6HCHhj+9JFF3KfU+Ej/j9A5gUz8d4` +
		`VYD92LXh82irI2tOelbQ7PZALn5hRnpk0gnpzLe2kpa8lGy0TtB/v/303jZAl0e7iKKkX1Ooy9JzeBAYdNqu9uI` +
		`hrl+naSXEGpuWxCY6uip+EcuKp9cR16kFzOKNn+sQ09d7KeFN9rLaZiodBzndVliq9FkWJwIeBp06Cq9tUMkmYv` +
		`UFk3mt86nhZrYviLI4tXSmZIGQNkvPQXU1ki3ukv/ZVdJrXV7w2k/EZmiF+9oxPwG4Z21GFfOKTWL/k9i7SCqIu` +
		`T41pNK2mmKHsQI7Qlortz54s8Y2DpzcKc4EDfUIf62No7++HIMxudPxbdQe11ImpDRcQIg9ZOqTboruLaGNBLO6` +
		`rdcnqmgts3CLrlex1L9QGxQZCHReariR1bWeQBNTAodYqSz6vpSI5hXUCkkXW7LFcPMqvRbRVclak8/Rp0LaHLQ` +
		`iDxkKpiQ1prfBO4IxYcHlEKPnxoBrk5WUZfX4j0Opbj8hkNVi+sB+/RvXruFLZoFAzcKu5KWKtPzUOb8P9VkEmN` +
		`y7D3JXys5Gfi8NkVXn/khVVb1BTHHf9wC3nhJ50nuBkmPu73bj1ABOajB+zAgMBAAGjgYMwgYAwDgYDVR0PAQH/` +
		`BAQDAgEGMB0GA1UdJQQWMBQGCCsGAQUFBwMBBggrBgEFBQcDAjAPBgNVHRMBAf8EBTADAQH/MB0GA1UdDgQWBBQ` +
		`/VsuyjgRDAEmcZjyJ77619Js9ijAfBgNVHSMEGDAWgBQ/VsuyjgRDAEmcZjyJ77619Js9ijANBgkqhkiG9w0BAQ` +
		`sFAAOCAgEAUc5QJOqxmMJY0E2rcHEWQYRah1vat3wuIHtEZ3SkSumyj+y9eyIHb9XTTyc4SyGyX1n8Rary8oSgQ` +
		`V4cbyJTFXEEQOGLHB9/98EKThgJtfPsos2WKe/59S8yN05onpxcaL9y4S295Kv9kcSQxLm5UfjlqsKeHJZymvxi` +
		`YzmBox7LA1zqcLYZvslJNkJxKAk5JA66iyDSQqOK7jIixn8pi305dFGCZglUFStwWqY6Rc9rR8EycVhSx2AhrvT` +
		`7OQTVdKLfoKA84D8JZJPB7hrxqKf7JJFs87Kjt7c/5bXPFJ2osmjoNYnbHjiq64bh20sSCd630qvhhePLwjjOlB` +
		`PiFyK36o/hQN871AEm1SCHy+aQcfJqF5KTgPnZQy5D+D/CGau+BfkO+WCGDVxRleYBJ4g2NbATolygB2KWXrj07` +
		`U/WaWqV2hERbkmxXFh6cUdlkX2MeoG4v6ZD2OKAPx5DpJCfp0TEq6PznP+Z1mLd/ZjGsOF8R2WGQJEuU8HRzvsr` +
		`0wsX9UyLMqf5XViDK11V/W+dcIvjHCayBpX2se3dfex5jFht+JcQc+iwB8caSXkR6tGSiargEtSJODORacO9IB8` +
		`b6W8Sm//JWf/8zyiCcMm1i2yVVphwE1kczFwunAh0JB896VaXGVxXeKEAMQoXHjgDdCYp8/Etxjb8UkCmyjU="]
		}
	]
}`

	invalidSpiffeX509Bundle = `
{
	"spiffe_sequence": 1,
	"spiffe_refresh_hint": 450000,
	"keys": [
		{
		"kty": "RSA",
		"use": "x509-svid",
		"n": "r10W2IcjT-vvSTpaFsS4OAcPOX87kw-zKZuJgXhxDhkOQyBdPZpUfK4H8yZ2q14Laym4bmiMLocIeGP70k` +
		`UXcp9T4SP-P0DmBTPx3hVgP3YteHzaKsja056VtDs9kAufmFGemTSCenMt7aSlryUbLRO0H-__fTeNkCXR7uIoq` +
		`RfU6jL0nN4EBh02q724iGuX6dpJcQam5bEJjq6Kn4Ry4qn1xHXqQXM4o2f6xDT13sp4U32stpmKh0HOd1WWKr0W` +
		`RYnAh4GnToKr21QySZi9QWTea3zqeFmti-Isji1dKZkgZA2S89BdTWSLe6S_9lV0mtdXvDaT8RmaIX72jE_Abhn` +
		`bUYV84pNYv-T2LtIKoi5PjWk0raaYoexAjtCWiu3PnizxjYOnNwpzgQN9Qh_rY2jv74cgzG50_Ft1B7XUiakNFx` +
		`AiD1k6pNuiu4toY0Es7qt1yeqaC2zcIuuV7HUv1AbFBkIdF5quJHVtZ5AE1MCh1ipLPq-lIjmFdQKSRdbssVw8y` +
		`q9FtFVyVqTz9GnQtoctCIPGQqmJDWmt8E7gjFhweUQo-fGgGuTlZRl9fiPQ6luPyGQ1WL6wH79G9eu4UtmgUDNw` +
		`q7kpYq0_NQ5vw_1WQSY3LsPclfKzkZ-Lw2RVef-SFVVvUFMcd_3ALeeEnnSe4GSY-7vduPUAE5qMH7M",
		"e": "AQAB"
		}
	]
}`

	// validRootCertFile, validIntCertFile and validWorkloadCertFile are in a certification chain.
	// They are generated using tools/certs/Makefile. Replace "cluster.local" with "foo.domain.com"
	// export INTERMEDIATE_DAYS=3650
	// export WORKLOAD_DAYS=3650
	// make foo-certs-selfSigned
	validRootCertFile1    = filepath.Join(env.IstioSrc, "security/pkg/pki/testdata/spiffe-root-cert-1.pem")
	validRootCertFile2    = filepath.Join(env.IstioSrc, "security/pkg/pki/testdata/spiffe-root-cert-2.pem")
	validIntCertFile      = filepath.Join(env.IstioSrc, "security/pkg/pki/testdata/spiffe-int-cert.pem")
	validWorkloadCertFile = filepath.Join(env.IstioSrc, "security/pkg/pki/testdata/spiffe-workload-cert.pem")
	validWorkloadKeyFile  = filepath.Join(env.IstioSrc, "security/pkg/pki/testdata/spiffe-workload-key.pem")
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

// The test starts one or two local servers and tests RetrieveSpiffeBundleRootCerts is able to correctly retrieve the
// SPIFFE bundles.
func TestRetrieveSpiffeBundleRootCertsFromStringInput(t *testing.T) {
	inputStringTemplate1 := `foo|URL1`
	inputStringTemplate2 := `foo|URL1||bar|URL2`
	totalRetryTimeout = time.Millisecond * 50
	testCases := []struct {
		name        string
		template    string
		trustCert   bool
		status      int
		body        string
		twoServers  bool
		errContains string
	}{
		{
			name:       "success",
			template:   inputStringTemplate1,
			trustCert:  true,
			status:     http.StatusOK,
			body:       validSpiffeX509Bundle,
			twoServers: false,
		},
		{
			name:       "success",
			template:   inputStringTemplate2,
			trustCert:  true,
			status:     http.StatusOK,
			body:       validSpiffeX509Bundle,
			twoServers: true,
		},
		{
			name:        "Invalid input 1",
			template:    "foo||URL1",
			trustCert:   false,
			status:      http.StatusOK,
			body:        validSpiffeX509Bundle,
			twoServers:  false,
			errContains: "config is invalid",
		},
		{
			name:        "Invalid input 2",
			template:    "foo|URL1|bar|URL2",
			trustCert:   false,
			status:      http.StatusOK,
			body:        validSpiffeX509Bundle,
			twoServers:  true,
			errContains: "config is invalid",
		},
		{
			name:        "Invalid input 3",
			template:    "URL1||bar|URL2",
			trustCert:   false,
			status:      http.StatusOK,
			body:        validSpiffeX509Bundle,
			twoServers:  true,
			errContains: "config is invalid",
		},
		{
			name:        "Unauthenticated cert",
			template:    inputStringTemplate1,
			trustCert:   false,
			status:      http.StatusOK,
			body:        validSpiffeX509Bundle,
			twoServers:  false,
			errContains: "x509: certificate signed by unknown authority",
		},
		{
			name:        "non-200 status",
			template:    inputStringTemplate1,
			trustCert:   true,
			status:      http.StatusServiceUnavailable,
			body:        "tHe SYsTEm iS DowN",
			twoServers:  false,
			errContains: "unexpected status: 503, fetching bundle: tHe SYsTEm iS DowN",
		},
		{
			name:        "Certificate absent",
			template:    inputStringTemplate1,
			trustCert:   true,
			status:      http.StatusOK,
			body:        invalidSpiffeX509Bundle,
			twoServers:  false,
			errContains: "expected 1 certificate in x509-svid entry 0; got 0",
		},
		{
			name:        "invalid bundle content",
			template:    inputStringTemplate1,
			trustCert:   true,
			status:      http.StatusOK,
			body:        "NOT JSON",
			twoServers:  false,
			errContains: "failed to decode bundle",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(testCase.status)
				_, _ = w.Write([]byte(testCase.body))
			})
			server := httptest.NewTLSServer(handler)
			input := strings.Replace(testCase.template, "URL1", server.Listener.Addr().String(), 1)
			var trustedCerts []*x509.Certificate
			if testCase.trustCert {
				trustedCerts = append(trustedCerts, server.Certificate())
			}
			if testCase.twoServers {
				input = strings.Replace(testCase.template, "URL1", server.Listener.Addr().String(), 1)
				handler2 := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					w.WriteHeader(testCase.status)
					_, _ = w.Write([]byte(testCase.body))
				})
				server2 := httptest.NewTLSServer(handler2)
				input = strings.Replace(input, "URL2", server2.Listener.Addr().String(), 1)
				if testCase.trustCert {
					trustedCerts = append(trustedCerts, server2.Certificate())
				}

			}
			rootCertMap, err := RetrieveSpiffeBundleRootCertsFromStringInput(input, trustedCerts)
			if testCase.errContains != "" {
				if !strings.Contains(err.Error(), testCase.errContains) {
					t.Errorf("unexpected error returned: %v. The error should contain: %s", err, testCase.errContains)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %s. Expected no error.", err)
			}
			if rootCertMap == nil {
				t.Errorf("returned root cert map is nil")
			}
		})
	}
}

// TestVerifyPeerCert tests VerifyPeerCert is effective at the client side, using a TLS server.
func TestGetGeneralCertPoolAndVerifyPeerCert(t *testing.T) {
	validRootCert := string(util.ReadFile(t, validRootCertFile1))
	validRootCert2 := string(util.ReadFile(t, validRootCertFile2))
	validIntCert := string(util.ReadFile(t, validIntCertFile))
	validWorkloadCert := string(util.ReadFile(t, validWorkloadCertFile))
	validWorkloadKey := string(util.ReadFile(t, validWorkloadKeyFile))

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Response body"))
	}))

	workloadCertBlock, _ := pem.Decode([]byte(validWorkloadCert))
	if workloadCertBlock == nil {
		t.Fatalf("failed to decode workload PEM cert")
	}
	intCertBlock, _ := pem.Decode([]byte(validIntCert))
	if intCertBlock == nil {
		t.Fatalf("failed to decode intermediate PEM cert")
	}
	serverCert := [][]byte{workloadCertBlock.Bytes, intCertBlock.Bytes}

	keyBlock, _ := pem.Decode([]byte(validWorkloadKey))
	if keyBlock == nil {
		t.Fatalf("failed to parse PEM block containing the workload private key")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		t.Fatalf("failed to parse workload private key: %v", privateKey)
	}

	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{
			{
				Certificate: serverCert,
				PrivateKey:  privateKey,
			},
		},
	}
	server.StartTLS()
	defer server.Close()

	testCases := []struct {
		name        string
		certMap     map[string][]string
		errContains string
	}{
		{
			name:    "Successful validation",
			certMap: map[string][]string{"foo.domain.com": {validRootCert}},
		},
		{
			name:    "Successful validation with multiple roots",
			certMap: map[string][]string{"foo.domain.com": {validRootCert, validRootCert2}},
		},
		{
			name: "Successful validation with multiple roots multiple mappings",
			certMap: map[string][]string{
				"foo.domain.com": {validRootCert, validRootCert2},
				"bar.domain.com": {validRootCert2},
			},
		},
		{
			name:        "No trusted root CA",
			certMap:     map[string][]string{"foo.domain.com": {validRootCert2}},
			errContains: "x509: certificate signed by unknown authority",
		},
		{
			name:        "Unknown trust domain",
			certMap:     map[string][]string{"bar.domain.com": {validRootCert}},
			errContains: "no cert pool found for trust domain foo.domain.com",
		},
		{
			name: "trustdomain not mapped to the needed root cert",
			certMap: map[string][]string{
				"foo.domain.com": {validRootCert2},
				"bar.domain.com": {validRootCert},
			},
			errContains: "x509: certificate signed by unknown authority",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			certMap := make(map[string][]*x509.Certificate)
			for trustDomain, certStrs := range testCase.certMap {
				certMap[trustDomain] = []*x509.Certificate{}
				for _, certStr := range certStrs {
					block, _ := pem.Decode([]byte(certStr))
					if block == nil {
						t.Fatalf("Can't decode the root cert.")
					}
					rootCert, err := x509.ParseCertificate(block.Bytes)
					if err != nil {
						t.Fatalf("Failed to parse certificate: " + err.Error())
					}
					certMap[trustDomain] = append(certMap[trustDomain], rootCert)
				}
			}

			verifier := NewPeerCertVerifier()
			verifier.AddMappings(certMap)
			if verifier == nil {
				t.Fatalf("Failed to create peer cert verifier.")
			}
			client := &http.Client{
				Timeout: time.Second,
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						RootCAs:               verifier.GetGeneralCertPool(),
						ServerName:            "foo.domain.com/ns/foo/sa/default",
						VerifyPeerCertificate: verifier.VerifyPeerCert,
					},
				},
			}

			req, err := http.NewRequest("POST", "https://"+server.Listener.Addr().String(), bytes.NewBuffer([]byte("ABC")))
			if err != nil {
				t.Errorf("failed to create HTTP client: %v", err)
			}
			_, err = client.Do(req)
			if testCase.errContains != "" {
				if err == nil {
					t.Errorf("Expected error should contain %s but seeing no error.", testCase.errContains)
				} else if !strings.Contains(err.Error(), testCase.errContains) {
					t.Errorf("unexpected error returned: %v. The error should contain: %s", err, testCase.errContains)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %s. Expected no error.", err)
			}
		})
	}
}

func TestExpandWithTrustDomains(t *testing.T) {
	testCases := []struct {
		name         string
		spiffeURI    []string
		trustDomains []string
		want         sets.Set
	}{
		{
			name:      "Basic",
			spiffeURI: []string{"spiffe://cluster.local/ns/def/sa/def"},
			trustDomains: []string{
				"foo",
			},
			want: map[string]struct{}{
				"spiffe://cluster.local/ns/def/sa/def": {},
				"spiffe://foo/ns/def/sa/def":           {},
			},
		},
		{
			name:      "InvalidInput",
			spiffeURI: []string{"spiffe:///abcdef", "spffff://a/b/c", "abcdef"},
			trustDomains: []string{
				"foo",
			},
			want: map[string]struct{}{
				"spiffe:///abcdef": {},
				"spffff://a/b/c":   {},
				"abcdef":           {},
			},
		},
		{
			name:         "EmptyTrustDomains",
			spiffeURI:    []string{"spiffe://cluster.local/ns/def/sa/def"},
			trustDomains: []string{},
			want: map[string]struct{}{
				"spiffe://cluster.local/ns/def/sa/def": {},
			},
		},
		{
			name:      "WithOriginalTrustDomain",
			spiffeURI: []string{"spiffe://cluster.local/ns/def/sa/def"},
			trustDomains: []string{
				"foo",
				"cluster.local",
			},
			want: map[string]struct{}{
				"spiffe://cluster.local/ns/def/sa/def": {},
				"spiffe://foo/ns/def/sa/def":           {},
			},
		},
		{
			name:      "TwoIentities",
			spiffeURI: []string{"spiffe://cluster.local/ns/def/sa/def", "spiffe://cluster.local/ns/a/sa/a"},
			trustDomains: []string{
				"foo",
			},
			want: map[string]struct{}{
				"spiffe://cluster.local/ns/def/sa/def": {},
				"spiffe://foo/ns/def/sa/def":           {},
				"spiffe://cluster.local/ns/a/sa/a":     {},
				"spiffe://foo/ns/a/sa/a":               {},
			},
		},
		{
			name:      "CustomIdentityFormat",
			spiffeURI: []string{"spiffe://cluster.local/custom-suffix"},
			trustDomains: []string{
				"foo",
			},
			want: map[string]struct{}{
				"spiffe://cluster.local/custom-suffix": {},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExpandWithTrustDomains(sets.New(tc.spiffeURI...), tc.trustDomains)
			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Errorf("unexpected expanded results: %v", diff)
			}
		})
	}
}

func TestIdentity(t *testing.T) {
	cases := []struct {
		input    string
		expected *Identity
	}{
		{
			"spiffe://td/ns/ns/sa/sa",
			&Identity{
				TrustDomain:    "td",
				Namespace:      "ns",
				ServiceAccount: "sa",
			},
		},
		{
			"spiffe://td.with.dots/ns/ns.with.dots/sa/sa.with.dots",
			&Identity{
				TrustDomain:    "td.with.dots",
				Namespace:      "ns.with.dots",
				ServiceAccount: "sa.with.dots",
			},
		},
		{
			// Empty ns and sa
			"spiffe://td/ns//sa/",
			&Identity{TrustDomain: "td", Namespace: "", ServiceAccount: ""},
		},
		{
			// Missing spiffe prefix
			"td/ns/ns/sa/sa",
			nil,
		},
		{
			// Missing SA
			"spiffe://td/ns/ns/sa",
			nil,
		},
		{
			// Trailing /
			"spiffe://td/ns/ns/sa/sa/",
			nil,
		},
		{
			// wrong separator /
			"spiffe://td/ns/ns/foobar/sa/",
			nil,
		},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseIdentity(tt.input)
			if tt.expected == nil {
				if err == nil {
					t.Fatalf("expected error, got %#v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, *tt.expected) {
				t.Fatalf("expected %#v, got %#v", *tt.expected, got)
			}

			roundTrip := got.String()
			if roundTrip != tt.input {
				t.Fatalf("round trip failed, expected %q got %q", tt.input, roundTrip)
			}
		})
	}
}
