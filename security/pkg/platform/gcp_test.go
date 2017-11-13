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
	"reflect"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	token = "abcdef"
)

// mockTokenFetcher implements the mock token fetcher.
type mockTokenFetcher struct {
	token        string
	errorMessage string
}

// A mock fetcher for FetchToken.
func (fetcher *mockTokenFetcher) FetchToken() (string, error) {
	if len(fetcher.errorMessage) > 0 {
		return "", fmt.Errorf(fetcher.errorMessage)
	}

	return fetcher.token, nil
}

func TestGetDialOptions(t *testing.T) {
	creds, err := credentials.NewClientTLSFromFile("testdata/cert-chain-good.pem", "")
	if err != nil {
		t.Errorf("Ubable to get credential for testdata/cert-chain-good.pem")
	}

	testCases := map[string]struct {
		cfg             GcpConfig
		token           string
		tokenFetchErr   string
		expectedErr     string
		expectedOptions []grpc.DialOption
	}{
		"nil configuration": {
			cfg: GcpConfig{
				RootCACertFile: "testdata/cert-chain-good.pem",
			},
			token:         "abcdef",
			expectedErr:   "Nil configuration passed",
			tokenFetchErr: "Nil configuration passed",
		},
		"Token fetch error": {
			cfg: GcpConfig{
				RootCACertFile: "testdata/cert-chain-good.pem",
			},
			token:         "",
			expectedErr:   "Nil configuration passed",
			tokenFetchErr: "Nil configuration passed",
		},
		"Root certificate file read error": {
			cfg: GcpConfig{
				RootCACertFile: "testdata/cert-chain-good_not_exist.pem",
			},
			token:         token,
			tokenFetchErr: "",
			expectedErr:   "open testdata/cert-chain-good_not_exist.pem: no such file or directory",
		},
		"Token fetched": {
			cfg: GcpConfig{
				RootCACertFile: "testdata/cert-chain-good.pem",
			},
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
			config:  c.cfg,
			fetcher: &mockTokenFetcher{c.token, c.tokenFetchErr},
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

		for index, option := range c.expectedOptions {
			if reflect.ValueOf(options[index]).Pointer() != reflect.ValueOf(option).Pointer() {
				t.Errorf("%s: Wrong option found", id)
			}
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
			// no need to move forward
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
		token       string
		expectedErr string
		expected    bool
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

func TestGcpIsProperPlatforms(t *testing.T) {
	testCases := map[string]struct {
		token         string
		tokenFetchErr string
		expected      bool
	}{
		"Good Identity": {
			expected: false,
		},
	}

	for id, c := range testCases {
		gcp := GcpClientImpl{GcpConfig{}, &mockTokenFetcher{c.token, c.tokenFetchErr}}

		isProperPlatform := gcp.IsProperPlatform()

		if isProperPlatform != c.expected {
			t.Errorf("%s: type Expected %v, Actual %v", id, c.expected, isProperPlatform)
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
		gcp := GcpClientImpl{GcpConfig{}, &mockTokenFetcher{c.token, c.tokenFetchErr}}

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

func TestGcpGetServiceIdentities(t *testing.T) {
	testCases := map[string]struct {
		token            string
		tokenFetchErr    string
		expectedErr      string
		expectedIdentity string
	}{
		"Good Identity": {
			token:            "abcdef",
			tokenFetchErr:    "",
			expectedErr:      "",
			expectedIdentity: "",
		},
	}

	for id, c := range testCases {
		gcp := GcpClientImpl{GcpConfig{}, &mockTokenFetcher{c.token, c.tokenFetchErr}}

		serviceIdentity, err := gcp.GetServiceIdentity()
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

		if string(c.expectedIdentity) != string(serviceIdentity) {
			t.Errorf("%s: identity Expected %v, Actual %v", id,
				string(c.expectedIdentity), string(serviceIdentity))
		}
	}
}

func TestGcpGetCredentialTypes(t *testing.T) {
	testCases := map[string]struct {
		cfg           GcpConfig
		token         string
		tokenFetchErr string
		expectedType  string
	}{
		"Good Identity": {
			cfg:          GcpConfig{},
			expectedType: "gcp",
		},
	}

	for id, c := range testCases {
		gcp := GcpClientImpl{
			config:  c.cfg,
			fetcher: &mockTokenFetcher{c.token, c.tokenFetchErr},
		}

		credentialType := gcp.GetCredentialType()
		if string(c.expectedType) != string(credentialType) {
			t.Errorf("%s: type Expected %v, Actual %v", id,
				string(c.expectedType), string(credentialType))
		}
	}
}
