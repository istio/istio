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
)

func TestNewClient(t *testing.T) {
	testCases := map[string]struct {
		platform     string
		cfg          ClientConfig
		caAddr       string
		expectedErr  string
		expectedType string
	}{
		"onprem test": {
			platform: "onprem",
			cfg: ClientConfig{
				OnPremConfig: OnPremConfig{
					RootCACertFile: "testdata/cert-chain-good.pem",
				},
			},
			caAddr:       "localhost",
			expectedErr:  "",
			expectedType: "onprem",
		},
		"gcp test": {
			platform: "gcp",
			cfg: ClientConfig{
				GcpConfig: GcpConfig{
					RootCACertFile: "testdata/cert-chain-good.pem",
				},
			},
			caAddr:       "localhost",
			expectedErr:  "",
			expectedType: "gcp",
		},
		"aws test": {
			platform: "aws",
			cfg: ClientConfig{
				AwsConfig: AwsConfig{
					RootCACertFile: "testdata/cert-chain-good.pem",
				},
			},
			caAddr:       "localhost",
			expectedErr:  "",
			expectedType: "aws",
		},
		"invalid test": {
			platform:    "invalid",
			expectedErr: "invalid env invalid specified",
		},
	}

	for id, tc := range testCases {
		client, err := NewClient(tc.platform, tc.cfg, tc.caAddr)
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
		if credentialType != tc.expectedType {
			t.Errorf("%s: Wrong Credential Type. Expected %v, Actual %v", id,
				string(tc.expectedType), string(credentialType))
		}
	}
}
