// Copyright Istio Authors
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

package credentialfetcher

import (
	"testing"

	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/credentialfetcher/plugin"
)

func TestNewCredFetcher(t *testing.T) {
	testCases := map[string]struct {
		fetcherType      string
		trustdomain      string
		jwtPath          string
		identityProvider string
		expectedErr      string
		expectedToken    string
		expectedIdp      string
	}{
		"gce test": {
			fetcherType:      security.GCE,
			trustdomain:      "abc.svc.id.goog",
			jwtPath:          "/var/run/secrets/tokens/istio-token",
			identityProvider: security.GCE,
			expectedErr:      "", // No error when ID token auth is enabled.
			expectedToken:    "",
			expectedIdp:      "GoogleComputeEngine",
		},
		"mock test": {
			fetcherType:      security.Mock,
			trustdomain:      "",
			jwtPath:          "",
			identityProvider: "fakeIDP",
			expectedErr:      "",
			expectedToken:    "test_token",
			expectedIdp:      "fakeIDP",
		},
		"invalid test": {
			fetcherType:      "foo",
			trustdomain:      "",
			jwtPath:          "",
			identityProvider: "",
			expectedErr:      "invalid credential fetcher type foo",
			expectedToken:    "",
			expectedIdp:      "",
		},
	}

	// Disable token refresh for GCE VM credential fetcher.
	plugin.SetTokenRotation(false)
	for id, tc := range testCases {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			cf, err := NewCredFetcher(
				tc.fetcherType, tc.trustdomain, tc.jwtPath, tc.identityProvider)
			if cf != nil {
				defer cf.Stop()
			}
			if len(tc.expectedErr) > 0 {
				if err == nil {
					t.Errorf("%s: succeeded. Error expected: %v", id, err)
				} else if err.Error() != tc.expectedErr {
					t.Errorf("%s: incorrect error message: %s VS %s",
						id, err.Error(), tc.expectedErr)
				}
			} else {
				if err != nil {
					t.Errorf("%s: unexpected Error: %v", id, err)
				}
				idp := cf.GetIdentityProvider()
				if idp != tc.expectedIdp {
					t.Errorf("%s: GetIdentityProvider returned %s, expected %s", id, idp, tc.expectedIdp)
				}
				if tc.fetcherType == security.Mock {
					token, err := cf.GetPlatformCredential()
					if err != nil {
						t.Errorf("%s: unexpected error calling GetPlatformCredential: %v", id, err)
					}
					if token != tc.expectedToken {
						t.Errorf("%s: GetPlatformCredential returned %s, expected %s", id, token, tc.expectedToken)
					}
				}
			}
		})
	}
	// Restore token refresh for other tests.
	plugin.SetTokenRotation(true)
}
