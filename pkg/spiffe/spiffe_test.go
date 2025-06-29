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

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/util/sets"
)

var (
	// nolint: lll
	validSpiffeX509Bundle = `{
  "keys": [
    {
      "use": "x509-svid",
      "kty": "EC",
      "crv": "P-256",
      "x": "8JAyuuX9TpQJUUCQdKIX4NUG5a2FmzWFORz-VEkET6k",
      "y": "HX1rdVwFy7NAsLcWtmt0D9IxtbwmU3oDJfji9T4ZXDs",
      "x5c": [
        "MIIBnTCCAUOgAwIBAgIBATAKBggqhkjOPQQDAjAkMSIwIAYDVQQDExlSb290IENBIGZvciB0cnVzdGRvbWFpbi5hMB4XDTA5MTExMDIzMDAwMFoXDTEwMTExMDIzMDAwMFowJDEiMCAGA1UEAxMZUm9vdCBDQSBmb3IgdHJ1c3Rkb21haW4uYTBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABPCQMrrl/U6UCVFAkHSiF+DVBuWthZs1hTkc/lRJBE+pHX1rdVwFy7NAsLcWtmt0D9IxtbwmU3oDJfji9T4ZXDujZjBkMA4GA1UdDwEB/wQEAwICBDASBgNVHRMBAf8ECDAGAQH/AgEBMB0GA1UdDgQWBBTm7xwcyW6KEkYeXRSFTn6Ngjcl9TAfBgNVHR4BAf8EFTAToBEwD4YNdHJ1c3Rkb21haW4uYTAKBggqhkjOPQQDAgNIADBFAiEA3uhdfxgrSehr+s7wSnD9QRpZjaiUcogPhAXyS73Qn9ACICUGj8pqxqfejMpdHEnz803lc6bHzaoUbd6cgemL7MRn"
      ]
    }
  ]
}`

	// nolint: lll
	validSpiffeX509BundleWithMultipleCerts = `{
  "keys": [
    {
      "use": "x509-svid",
      "kty": "EC",
      "crv": "P-256",
      "x": "HFlg42KnDPaiGvQrAIaKWDqJw_4ngCwZ_687jLrBUVE",
      "y": "ActNT7SNmcX3tQD9YZgRueiajOgmYv-rANQ8_H8GBEU",
      "x5c": [
        "MIIBnTCCAUOgAwIBAgIBAjAKBggqhkjOPQQDAjAkMSIwIAYDVQQDExlSb290IENBIGZvciB0cnVzdGRvbWFpbi5iMB4XDTA5MTExMDIzMDAwMFoXDTEwMTExMDIzMDAwMFowJDEiMCAGA1UEAxMZUm9vdCBDQSBmb3IgdHJ1c3Rkb21haW4uYjBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABBxZYONipwz2ohr0KwCGilg6icP+J4AsGf+vO4y6wVFRActNT7SNmcX3tQD9YZgRueiajOgmYv+rANQ8/H8GBEWjZjBkMA4GA1UdDwEB/wQEAwICBDASBgNVHRMBAf8ECDAGAQH/AgEBMB0GA1UdDgQWBBTR41Rmvs/3JBw24dDjlwgLuFB13zAfBgNVHR4BAf8EFTAToBEwD4YNdHJ1c3Rkb21haW4uYjAKBggqhkjOPQQDAgNIADBFAiEA243KZVSU5IUTmoj0OCvcBYnKo3a1p/kQal1qqFcE0BgCIBZB+2OJU/dPRs1AoGilH6AZqVC5KZlSPZq9bv6Pm5UG"
      ]
    },
    {
      "use": "x509-svid",
      "kty": "EC",
      "crv": "P-256",
      "x": "vzhUQgVXLcdwrDmP6REclI3lusWR6MHM6i5bXXq87Pk",
      "y": "-407emssjw1NjyK1gs33mczZapiqHoWMesi2sUudgsw",
      "x5c": [
        "MIIBnTCCAUOgAwIBAgIBAjAKBggqhkjOPQQDAjAkMSIwIAYDVQQDExlSb290IENBIGZvciB0cnVzdGRvbWFpbi5iMB4XDTA5MTExMDIzMDAwMFoXDTEwMTExMDIzMDAwMFowJDEiMCAGA1UEAxMZUm9vdCBDQSBmb3IgdHJ1c3Rkb21haW4uYjBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABL84VEIFVy3HcKw5j+kRHJSN5brFkejBzOouW116vOz5+407emssjw1NjyK1gs33mczZapiqHoWMesi2sUudgsyjZjBkMA4GA1UdDwEB/wQEAwICBDASBgNVHRMBAf8ECDAGAQH/AgEBMB0GA1UdDgQWBBQSZLzOBu77lDJGEP5uxqAJgze0+zAfBgNVHR4BAf8EFTAToBEwD4YNdHJ1c3Rkb21haW4uYjAKBggqhkjOPQQDAgNIADBFAiA9KU1cfQilqatMt7cWoIPUeq72rso1B0RAN9MxcZV9ugIhAOHbu0dHR1Pq3iuHCrwSCiM1xQ+fnay6m64B/Hv78q2W"
      ]
    }
  ]
}`

	invalidSpiffeX509Bundle = `{
  "keys": [
    {
      "use": "x509-svid",
      "kty": "EC",
      "crv": "P-256",
      "x": "8JAyuuX9TpQJUUCQdKIX4NUG5a2FmzWFORz-VEkET6k",
      "y": "HX1rdVwFy7NAsLcWtmt0D9IxtbwmU3oDJfji9T4ZXDs"
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
		got, err := genSpiffeURI(tc.trustDomain, tc.namespace, tc.serviceAccount)
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

func TestMustGenSpiffeURI(t *testing.T) {
	mesh := &meshconfig.MeshConfig{TrustDomain: "something.local"}
	if nonsense := MustGenSpiffeURI(mesh, "", ""); nonsense != "spiffe://something.local/ns//sa/" {
		t.Errorf("Unexpected spiffe URI for empty namespace and service account: %s", nonsense)
	}
}

type handler struct {
	statusCode int
	body       []byte
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(h.statusCode)
	w.Write(h.body)
}

func TestRetrieveSpiffeBundleRootCerts(t *testing.T) {
	// Create a fake handler whose response can be overridden for each test case.
	h := &handler{}

	// Create servers that will act as SPIFFE bundle endpoints.
	s1 := httptest.NewTLSServer(h)
	s2 := httptest.NewTLSServer(h)

	// The system needs to trust these server certs to securely connect to
	// the SPIFFE bundle endpoints.
	serverCerts := []*x509.Certificate{s1.Certificate(), s2.Certificate()}

	input1 := map[string]string{
		"foo": s1.Listener.Addr().String(),
	}
	// This simulates the case when there are multiple different servers to talk to.
	input2 := map[string]string{
		"foo": s1.Listener.Addr().String(),
		"bar": s2.Listener.Addr().String(),
	}

	cases := []struct {
		name         string
		in           map[string]string
		extraCerts   []*x509.Certificate
		statusCode   int
		body         string
		errContains  string
		wantNumCerts int
	}{
		{
			name:         "Success with one trust domain",
			in:           input1,
			extraCerts:   serverCerts,
			statusCode:   http.StatusOK,
			body:         validSpiffeX509Bundle,
			wantNumCerts: 1,
		},
		{
			name:         "Success with multiple trust domains",
			in:           input2,
			extraCerts:   serverCerts,
			statusCode:   http.StatusOK,
			body:         validSpiffeX509Bundle,
			wantNumCerts: 1,
		},
		{
			name:         "Success when response contains multiple certs",
			in:           input1,
			extraCerts:   serverCerts,
			statusCode:   http.StatusOK,
			body:         validSpiffeX509BundleWithMultipleCerts,
			wantNumCerts: 2,
		},
		{
			name:        "Bundle endpoint is not trusted",
			in:          input1,
			extraCerts:  nil,
			statusCode:  http.StatusOK,
			body:        validSpiffeX509Bundle,
			errContains: "x509: certificate signed by unknown authority",
		},
		{
			name:        "Bundle endpoint returns non-200 status",
			in:          input1,
			extraCerts:  serverCerts,
			statusCode:  http.StatusServiceUnavailable,
			body:        `{"error": "system down"}`,
			errContains: `unexpected status: 503, fetching bundle: {"error": "system down"}`,
		},
		{
			name:        "Bundle contains no certificate",
			in:          input1,
			extraCerts:  serverCerts,
			statusCode:  http.StatusOK,
			body:        invalidSpiffeX509Bundle,
			errContains: "expected 1 certificate in x509-svid entry 0; got 0",
		},
		{
			name:        "Bundle cannot be decoded",
			in:          input1,
			extraCerts:  serverCerts,
			statusCode:  http.StatusOK,
			body:        "NOT JSON",
			errContains: "failed to decode bundle",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.body = []byte(c.body)
			h.statusCode = c.statusCode

			caCertPool, err := x509.SystemCertPool()
			if err != nil {
				t.Fatalf("failed to get SystemCertPool: %v", err)
			}
			for _, cert := range c.extraCerts {
				caCertPool.AddCert(cert)
			}

			// This is the system-under-test.
			rootCertMap, err := RetrieveSpiffeBundleRootCerts(c.in, caCertPool, time.Millisecond*50)

			if c.errContains != "" {
				if err == nil {
					t.Fatalf("got nil error; wanted error to contain %q", c.errContains)
				}
				if !strings.Contains(err.Error(), c.errContains) {
					t.Fatalf("got error: %q; wanted error to contain %q", err, c.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("got error: %q; wanted no error", err)
				}
				if rootCertMap == nil {
					t.Errorf("returned root cert map is nil")
				}
				for k, v := range rootCertMap {
					if len(v) != c.wantNumCerts {
						t.Errorf("got %d certs for %s; wanted %d certs", len(v), k, c.wantNumCerts)
					}
				}
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
		t.Fatal("failed to decode workload PEM cert")
	}
	intCertBlock, _ := pem.Decode([]byte(validIntCert))
	if intCertBlock == nil {
		t.Fatal("failed to decode intermediate PEM cert")
	}
	serverCert := [][]byte{workloadCertBlock.Bytes, intCertBlock.Bytes}

	keyBlock, _ := pem.Decode([]byte(validWorkloadKey))
	if keyBlock == nil {
		t.Fatal("failed to parse PEM block containing the workload private key")
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
		MinVersion: tls.VersionTLS12,
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
						t.Fatal("Can't decode the root cert.")
					}
					rootCert, err := x509.ParseCertificate(block.Bytes)
					if err != nil {
						t.Fatal("Failed to parse certificate: " + err.Error())
					}
					certMap[trustDomain] = append(certMap[trustDomain], rootCert)
				}
			}

			verifier := NewPeerCertVerifier()
			verifier.AddMappings(certMap)
			if verifier == nil {
				t.Fatal("Failed to create peer cert verifier.")
			}
			client := &http.Client{
				Timeout: time.Second,
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						RootCAs:               verifier.GetGeneralCertPool(),
						ServerName:            "foo.domain.com/ns/foo/sa/default",
						VerifyPeerCertificate: verifier.VerifyPeerCert,
						MinVersion:            tls.VersionTLS12,
					},
				},
			}

			req, err := http.NewRequest(http.MethodPost, "https://"+server.Listener.Addr().String(), bytes.NewBuffer([]byte("ABC")))
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
		want         sets.String
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
		{
			name:      "Non SPIFFE URI",
			spiffeURI: []string{"testdns.com"},
			trustDomains: []string{
				"foo",
			},
			want: map[string]struct{}{
				"testdns.com": {},
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
			// Wrong ns separator
			"spiffe://td/foobar/ns/sa/sa",
			nil,
		},
		{
			// Wrong sa separator
			"spiffe://td/ns/ns/foobar/sa",
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

func TestGetExtraTrustDomains(t *testing.T) {
	mesh := &meshconfig.MeshConfig{
		CaCertificates: []*meshconfig.MeshConfig_CertificateData{
			{
				TrustDomains: []string{"b", "a"},
			},
			{
				TrustDomains: []string{"c"},
			},
		},
	}
	if result := GetExtraTrustDomains(mesh); !slices.Equal(result, []string{"b", "a", "c"}) {
		t.Errorf("Unexpected trust domains. expected: [b a c], got: %v", result)
	}
}
