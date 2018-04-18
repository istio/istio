// Copyright 2017 Istio Authors
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

package platform

import (
	"bytes"
	"io/ioutil"
	"strings"
	"testing"
)

func TestNewOnPremClientImpl(t *testing.T) {
	testCases := map[string]struct {
	}{}
	for id, c := range testCases {
		t.Log(id, c)
	}
}

func TestOnPremGetServiceIdentity(t *testing.T) {
	testCases := map[string]struct {
		rootCert    string
		keyFile     string
		filename    string
		expectedID  string
		expectedErr string
	}{
		"Good cert1": {
			rootCert:   "testdata/cert-root-good.pem",
			keyFile:    "testdata/key-from-root-good.pem",
			filename:   "testdata/cert-chain-good.pem",
			expectedID: "spiffe://cluster.local/ns/default/sa/default",
		},
		"Good cert2": {
			rootCert:   "testdata/cert-root-good.pem",
			keyFile:    "testdata/key-from-root-good.pem",
			filename:   "testdata/cert-chain-good2.pem",
			expectedID: "spiffe://cluster.local/ns/default/sa/default",
		},
		"Bad cert format": {
			rootCert:    "testdata/cert-root-good.pem",
			keyFile:     "testdata/key-from-root-good.pem",
			filename:    "testdata/cert-chain-bad1.pem",
			expectedErr: "invalid PEM encoded certificate",
		},
		"Wrong file": {
			rootCert:    "testdata/cert-root-good.pem",
			keyFile:     "testdata/key-from-root-good.pem",
			filename:    "testdata/cert-chain-bad2.pem",
			expectedErr: "testdata/cert-chain-bad2.pem: no such file or directory",
		},
	}

	for id, c := range testCases {
		var identity string
		onprem, err := NewOnPremClientImpl(c.rootCert, c.keyFile, c.filename)
		if err == nil {
			identity, err = onprem.GetServiceIdentity()
		}
		if c.expectedErr == "" && err != nil {
			t.Errorf("%v got error %v, want no error", id, err)
		}
		if c.expectedErr != "" {
			if err == nil {
				t.Errorf("%v: no error is returtned, want %v", id, c.expectedErr)
			} else if !strings.Contains(err.Error(), c.expectedErr) {
				t.Errorf("%v: %v %v", id, err, c.expectedErr)
			}
			continue
		}
		if identity != c.expectedID {
			t.Errorf("%s: GetServiceIdentity returns identity: %s. It should be %s.", id, identity, c.expectedID)
		}
	}
}

func TestGetTLSCredentials(t *testing.T) {
	testCases := map[string]struct {
		rootCertFile  string
		certChainFile string
		keyFile       string
		expectedErr   string
	}{
		"Good cert": {
			rootCertFile:  "testdata/cert-root-good.pem",
			certChainFile: "testdata/cert-from-root-good.pem",
			keyFile:       "testdata/key-from-root-good.pem",
			expectedErr:   "",
		},
		"Loading failure": {
			rootCertFile:  "testdata/cert-root-good.pem",
			certChainFile: "testdata/cert-from-root-goo.pem",
			keyFile:       "testdata/key-from-root-not-exist.pem",
			expectedErr:   "testdata/key-from-root-not-exist.pem: no such file or directory",
		},
		"Loading root cert failure": {
			rootCertFile:  "testdata/cert-root-not-exist.pem",
			certChainFile: "testdata/cert-from-root-good.pem",
			keyFile:       "testdata/key-from-root-good.pem",
			expectedErr:   "testdata/cert-root-not-exist.pem: no such file or directory",
		},
	}

	for id, c := range testCases {
		onprem, err := NewOnPremClientImpl(c.rootCertFile, c.keyFile, c.certChainFile)
		if err == nil {
			_, err = onprem.GetDialOptions()
		}
		if c.expectedErr == "" && err != nil {
			t.Errorf("%v got error %v, want no error", id, err)
		}
		if c.expectedErr != "" {
			if err == nil {
				t.Errorf("%v: no error is returned, want %v", id, c.expectedErr)
			} else if !strings.Contains(err.Error(), c.expectedErr) {
				t.Errorf("%v: unexpected error got %v want contains %v", id, err, c.expectedErr)
			}
		}
	}
}

func TestGetAgentCredential(t *testing.T) {
	certFile := "testdata/cert-chain.pem"
	certBytes, err := ioutil.ReadFile(certFile)
	if err != nil {
		t.Fatalf("unable to read file %s", certFile)
	}

	testCases := map[string]struct {
		rootCertFile  string
		keyFile       string
		filename      string
		expectedBytes []byte
		expectedErr   string
	}{
		"Existing cert": {
			rootCertFile:  "testdata/cert-root-good.pem",
			keyFile:       "testdata/key-from-root-good.pem",
			filename:      certFile,
			expectedBytes: certBytes,
			expectedErr:   "",
		},
		"Missing cert": {
			rootCertFile:  "testdata/cert-root-good.pem",
			keyFile:       "testdata/key-from-root-good.pem",
			filename:      "testdata/fake-cert.pem",
			expectedBytes: nil,
			expectedErr:   "testdata/fake-cert.pem: no such file or directory",
		},
	}

	for id, c := range testCases {
		onprem, err := NewOnPremClientImpl(c.rootCertFile, c.keyFile, c.filename)
		var cred []byte
		if err == nil {
			cred, err = onprem.GetAgentCredential()
		}
		if c.expectedErr == "" && err != nil {
			t.Errorf("%v got error %v, want no error", id, err)
		}
		if c.expectedErr != "" {
			if err == nil {
				t.Errorf("%v: no error is returned, want %v", id, c.expectedErr)
			} else if !strings.Contains(err.Error(), c.expectedErr) {
				t.Errorf("%v: unexpected error got %v want contains %v", id, err, c.expectedErr)
			}
			continue
		}
		if !bytes.Equal(cred, c.expectedBytes) {
			t.Errorf("%s: GetAgentCredential returns bytes: %s. It should be %s.", id, cred, c.expectedBytes)
		}
	}
}

func TestOnpremIsProperPlatform(t *testing.T) {
	onprem, err := NewOnPremClientImpl(
		"testdata/cert-root-good.pem", "testdata/key-from-root-good.pem", "testdata/cert-from-root-good.pem")
	if err != nil {
		t.Errorf("failed to create OnPrem client %v", err)
	}
	exptected := onprem.IsProperPlatform()
	if !exptected {
		t.Errorf("Unexpected response: %v.", exptected)
	}
}

func TestOnpremGetCredentialType(t *testing.T) {
	onprem, err := NewOnPremClientImpl(
		"testdata/cert-root-good.pem", "testdata/key-from-root-good.pem", "testdata/cert-from-root-good.pem")
	if err != nil {
		t.Errorf("failed to create onprem client %v", err)
	}
	credentialType := onprem.GetCredentialType()
	if credentialType != "onprem" {
		t.Errorf("Unexpected credential type: %v.", credentialType)
	}
}
