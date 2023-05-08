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
