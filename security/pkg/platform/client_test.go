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
	"testing"

	"cloud.google.com/go/compute/metadata"
)

func TestNewClient(t *testing.T) {
	testCases := map[string]struct {
		platform      string
		rootCertFile  string
		keyFile       string
		certChainFile string
		caAddr        string
		expectedErr   string
	}{
		"onprem test": {
			platform:      "onprem",
			rootCertFile:  "testdata/cert-root-good.pem",
			keyFile:       "testdata/key-from-root-good.pem",
			certChainFile: "testdata/cert-from-root-good.pem",
			caAddr:        "localhost",
			expectedErr:   "",
		},
		"gcp test": {
			platform:      "gcp",
			rootCertFile:  "testdata/cert-root-good.pem",
			keyFile:       "testdata/key-from-root-good.pem",
			certChainFile: "testdata/cert-chain-good.pem",
			caAddr:        "localhost",
			expectedErr:   "GCP credential authentication in CSR API is disabled", // No error when ID token auth is enabled.
		},
		"aws test": {
			platform:      "aws",
			rootCertFile:  "testdata/cert-root-good.pem",
			keyFile:       "testdata/key-from-root-good.pem",
			certChainFile: "testdata/cert-chain-good.pem",
			caAddr:        "localhost",
			expectedErr:   "AWS credential authentication in CSR API is disabled", // No error when ID token auth is enabled.
		},
		"unspecified test": {
			platform:      "unspecified",
			rootCertFile:  "testdata/cert-root-good.pem",
			keyFile:       "testdata/key-from-root-good.pem",
			certChainFile: "testdata/cert-chain-good.pem",
			caAddr:        "localhost",
			expectedErr:   "",
		},
		"invalid test": {
			platform:    "invalid",
			expectedErr: "invalid env invalid specified",
		},
	}

	for id, tc := range testCases {
		client, err := NewClient(
			tc.platform, tc.rootCertFile, tc.keyFile, tc.certChainFile, tc.caAddr)
		if len(tc.expectedErr) > 0 {
			if err == nil {
				t.Errorf("%s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != tc.expectedErr {
				t.Errorf("%s: incorrect error message: %s VS %s",
					id, err.Error(), tc.expectedErr)
			}
			continue
		} else if err != nil {
			t.Fatalf("%s: Unexpected Error: %v", id, err)
		}

		credentialType := client.GetCredentialType()
		expectedType := tc.platform
		if expectedType == "unspecified" {
			if metadata.OnGCE() {
				expectedType = "gcp"
			} else {
				expectedType = "onprem"
			}
		}
		if credentialType != expectedType {
			t.Errorf("%s: Wrong Credential Type. Expected %v, Actual %v", id,
				string(expectedType), string(credentialType))
		}
	}
}
