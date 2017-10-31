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
	"testing"
)

func TestGetServiceIdentity(t *testing.T) {
	testCases := map[string]struct {
		filename    string
		expectedID  string
		expectedErr string
	}{
		"Good cert1": {
			filename:    "testdata/cert-chain-good.pem",
			expectedID:  "spiffe://cluster.local/ns/default/sa/default",
			expectedErr: "",
		},
		"Good cert2": {
			filename:    "testdata/cert-chain-good2.pem",
			expectedID:  "spiffe://cluster.local/ns/default/sa/default",
			expectedErr: "",
		},
		"Bad cert format": {
			filename:    "testdata/cert-chain-bad1.pem",
			expectedID:  "",
			expectedErr: "Invalid PEM encoded certificate",
		},
		"Wrong file": {
			filename:    "testdata/cert-chain-bad2.pem",
			expectedID:  "",
			expectedErr: "open testdata/cert-chain-bad2.pem: no such file or directory",
		},
	}

	for id, c := range testCases {
		onprem := OnPremClientImpl{c.filename}
		identity, err := onprem.GetServiceIdentity()
		if c.expectedErr != "" {
			if err == nil {
				t.Errorf("%s: no error is returned.", id)
			} else if err.Error() != c.expectedErr {
				t.Errorf("%s: incorrect error message: %s VS %s", id, err.Error(), c.expectedErr)
			}
		} else if identity != c.expectedID {
			t.Errorf("%s: GetServiceIdentity returns identity: %s. It should be %s.", id, identity, c.expectedID)
		}
	}
}

func TestGetTLSCredentials(t *testing.T) {
	testCases := map[string]struct {
		config      *ClientConfig
		expectedErr string
	}{
		"Good cert": {
			config: &ClientConfig{
				CertChainFile:  "testdata/cert-from-root-good.pem",
				KeyFile:        "testdata/key-from-root-good.pem",
				RootCACertFile: "testdata/cert-root-good.pem",
			},
			expectedErr: "",
		},
		"Loading failure": {
			config: &ClientConfig{
				CertChainFile:  "testdata/cert-from-root-goo.pem",
				KeyFile:        "testdata/cert-from-root-not-exist.pem",
				RootCACertFile: "testdata/cert-root-good.pem",
			},
			expectedErr: "Cannot load key pair: open testdata/cert-from-root-goo.pem: no such file or directory",
		},
		"Loading root cert failure": {
			config: &ClientConfig{
				CertChainFile:  "testdata/cert-from-root-good.pem",
				KeyFile:        "testdata/key-from-root-good.pem",
				RootCACertFile: "testdata/cert-root-not-exist.pem",
			},
			expectedErr: "Failed to read CA cert: open testdata/cert-root-not-exist.pem: no such file or directory",
		},
	}

	for id, c := range testCases {
		onprem := OnPremClientImpl{""}

		_, err := onprem.GetDialOptions(c.config)
		if len(c.expectedErr) > 0 {
			if err == nil {
				t.Errorf("%s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != c.expectedErr {
				t.Errorf("%s: incorrect error message: %s VS %s", id, err.Error(), c.expectedErr)
			}
		} else if err != nil {
			t.Errorf("%s: Unexpected Error: %v", id, err)
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
		filename      string
		expectedBytes []byte
		expectedErr   string
	}{
		"Existing cert": {
			filename:      certFile,
			expectedBytes: certBytes,
			expectedErr:   "",
		},
		"Missing cert": {
			filename:      "testdata/fake-cert.pem",
			expectedBytes: nil,
			expectedErr:   "Failed to read cert file: testdata/fake-cert.pem",
		},
	}

	for id, c := range testCases {
		onprem := OnPremClientImpl{c.filename}
		cred, err := onprem.GetAgentCredential()
		if c.expectedErr != "" {
			if err == nil {
				t.Errorf("%s: no error is returned.", id)
			} else if err.Error() != c.expectedErr {
				t.Errorf("%s: incorrect error message: %s VS %s", id, err.Error(), c.expectedErr)
			}
		} else if !bytes.Equal(cred, c.expectedBytes) {
			t.Errorf("%s: GetAgentCredential returns bytes: %s. It should be %s.", id, cred, c.expectedBytes)
		}
	}
}
