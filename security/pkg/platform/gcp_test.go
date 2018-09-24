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
	"context"
	"fmt"
	"istio.io/istio/pkg/spiffe"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	token = "abcdef"
)

// mockTokenFetcher implements the mock token fetcher.
type mockTokenFetcher struct {
	token                  string
	tokenErrorMsg          string
	serviceAccount         string
	serviceAccountErrorMsg string
}

// A mock fetcher for FetchToken.
func (fetcher *mockTokenFetcher) FetchToken() (string, error) {
	if len(fetcher.tokenErrorMsg) > 0 {
		return "", fmt.Errorf(fetcher.tokenErrorMsg)
	}

	return fetcher.token, nil
}

// A mock fetcher for FetchToken.
func (fetcher *mockTokenFetcher) FetchServiceAccount() (string, error) {
	if len(fetcher.serviceAccountErrorMsg) > 0 {
		return "", fmt.Errorf(fetcher.serviceAccountErrorMsg)
	}

	return fetcher.serviceAccount, nil
}

func TestGcpGetServiceIdentity(t *testing.T) {
	spiffe.WithIdentityDomain("cluster.local", func() {
		testCases := map[string]struct {
			rootCertFile string
			sa           string
			err          string
			expectedSa   string
			expectedErr  string
		}{
			"success": {
				rootCertFile: "testdata/cert-chain-good.pem",
				sa:           "464382306716@developer.gserviceaccount.com",
				expectedSa:   "spiffe://cluster.local/ns/default/sa/464382306716@developer.gserviceaccount.com",
			},
			"fetch error": {
				rootCertFile: "testdata/cert-chain-good.pem",
				err:          "failed to fetch service account",
				expectedErr:  "failed to fetch service account",
			},
		}

		for id, c := range testCases {
			gcp := GcpClientImpl{
				rootCertFile: c.rootCertFile,
				fetcher:      &mockTokenFetcher{"", "", c.sa, c.err},
			}

			actualSa, err := gcp.GetServiceIdentity()
			if len(c.expectedErr) > 0 {
				if err == nil {
					t.Errorf("%s: Succeeded. Error expected: %v", id, err)
				} else if err.Error() != c.expectedErr {
					t.Errorf("%s: incorrect error message: %s VS %s", id, err.Error(), c.expectedErr)
				}
			} else if err != nil {
				t.Fatalf("%s: Unexpected Error: %v", id, err)
			}

			// Make sure there're two dial options, one for TLS and one for JWT.
			if actualSa != c.expectedSa {
				t.Errorf("%s: Wrong Service Account. Expected %v, Actual %v", id, c.expectedSa, actualSa)
			}
		}

	})
}

func TestGetDialOptions(t *testing.T) {
	creds, err := credentials.NewClientTLSFromFile("testdata/cert-chain-good.pem", "")
	assert.Equal(t, err, nil, "Unable to get credential for testdata/cert-chain-good.pem")

	testCases := map[string]struct {
		rootCertFile    string
		token           string
		tokenFetchErr   string
		expectedErr     string
		expectedOptions []grpc.DialOption
	}{
		"nil configuration": {
			rootCertFile:  "testdata/cert-chain-good.pem",
			token:         "abcdef",
			expectedErr:   "Nil configuration passed",
			tokenFetchErr: "Nil configuration passed",
		},
		"Token fetch error": {
			rootCertFile:  "testdata/cert-chain-good.pem",
			token:         "",
			expectedErr:   "Nil configuration passed",
			tokenFetchErr: "Nil configuration passed",
		},
		"Root certificate file read error": {
			rootCertFile:  "testdata/cert-chain-good_not_exist.pem",
			token:         token,
			tokenFetchErr: "",
			expectedErr:   "open testdata/cert-chain-good_not_exist.pem: no such file or directory",
		},
		"Token fetched": {
			rootCertFile:  "testdata/cert-chain-good.pem",
			token:         token,
			tokenFetchErr: "",
			expectedOptions: []grpc.DialOption{
				grpc.WithPerRPCCredentials(&jwtAccess{token}),
				grpc.WithTransportCredentials(creds),
			},
		},
	}

	for id, c := range testCases {
		gcp := GcpClientImpl{
			rootCertFile: c.rootCertFile,
			fetcher:      &mockTokenFetcher{c.token, c.tokenFetchErr, "", ""},
		}

		options, err := gcp.GetDialOptions()
		if len(c.expectedErr) > 0 {
			if err == nil {
				t.Errorf("%s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != c.expectedErr {
				t.Errorf("%s: incorrect error message: %s VS %s",
					id, err.Error(), c.expectedErr)
			}
			continue
		} else if err != nil {
			t.Fatalf("%s: Unexpected Error: %v", id, err)
		}

		// Make sure there're two dial options, one for TLS and one for JWT.
		if len(options) != len(c.expectedOptions) {
			t.Errorf("%s: Wrong dial options size. Expected %v, Actual %v", id, len(c.expectedOptions), len(options))
		}
	}
}

func TestGcpGetRequestMetadata(t *testing.T) {
	testCases := map[string]struct {
		token       string
		expectedErr string
		expected    map[string]string
	}{
		"Good Identity": {
			token:       "token",
			expectedErr: "",
			expected: map[string]string{
				httpAuthHeader: "Bearer token",
			},
		},
	}

	for id, c := range testCases {
		jwt := jwtAccess{
			token: c.token,
		}

		metadata, err := jwt.GetRequestMetadata(context.Background(), "")
		if len(c.expectedErr) > 0 {
			if err == nil {
				t.Errorf("%s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != c.expectedErr {
				t.Errorf("%s: incorrect error message: %s VS %s",
					id, err.Error(), c.expectedErr)
			}
			continue
		} else if err != nil {
			t.Fatalf("%s: Unexpected Error: %v", id, err)
		}

		if !reflect.DeepEqual(c.expected, metadata) {
			t.Errorf("%s: metadata Expected %v, Actual %v", id, c.expected, metadata)
		}
	}
}

func TestGcpRequireTransportSecurity(t *testing.T) {
	testCases := map[string]struct {
		token    string
		expected bool
	}{
		"Expected true": {
			expected: true,
		},
	}

	for id, c := range testCases {
		jwt := jwtAccess{
			token: c.token,
		}
		requireTransportSecurity := jwt.RequireTransportSecurity()
		if c.expected != requireTransportSecurity {
			t.Errorf("%s: Expected %v, Actual %v", id, c.expected, requireTransportSecurity)
		}
	}
}

func TestGcpGetAgentCredentials(t *testing.T) {
	testCases := map[string]struct {
		token              string
		tokenFetchErr      string
		expectedErr        string
		expectedCredential []byte
	}{
		"Token abddef is exptected": {
			token:              "abcdef",
			tokenFetchErr:      "",
			expectedErr:        "",
			expectedCredential: []byte("abcdef"),
		},
		"Failed to fetch token": {
			token:              "abcdef",
			tokenFetchErr:      "Token Ftch Error",
			expectedErr:        "Token Ftch Error",
			expectedCredential: []byte(""),
		},
	}

	for id, c := range testCases {
		gcp := GcpClientImpl{"", "", &mockTokenFetcher{c.token, c.tokenFetchErr, "", ""}}

		credential, err := gcp.GetAgentCredential()
		if len(c.expectedErr) > 0 {
			if err == nil {
				t.Errorf("%s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != c.expectedErr {
				t.Errorf("%s: incorrect error message: %s VS %s",
					id, err.Error(), c.expectedErr)
			}
			continue
		} else if err != nil {
			t.Fatalf("%s: Unexpected Error: %v", id, err)
		}

		if string(c.expectedCredential) != string(credential) {
			t.Errorf("%s: credential Expected %v, Actual %v", id,
				string(c.expectedCredential), string(credential))
		}
	}
}

func TestGcpGetCredentialTypes(t *testing.T) {
	testCases := map[string]struct {
		rootCertFile  string
		token         string
		tokenFetchErr string
		expectedType  string
	}{
		"Good Identity": {
			rootCertFile: "",
			expectedType: "gcp",
		},
	}

	for id, c := range testCases {
		gcp := GcpClientImpl{
			rootCertFile: c.rootCertFile,
			fetcher:      &mockTokenFetcher{c.token, c.tokenFetchErr, "", ""},
		}

		credentialType := gcp.GetCredentialType()
		if string(c.expectedType) != string(credentialType) {
			t.Errorf("%s: type Expected %v, Actual %v", id,
				string(c.expectedType), string(credentialType))
		}
	}
}
