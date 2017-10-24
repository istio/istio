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
		cfg             *ClientConfig
		token           string
		tokenFetchErr   string
		expectedErr     string
		expectedOptions []grpc.DialOption
	}{
		"nil configuration": {
			cfg: &ClientConfig{
				RootCACertFile: "testdata/cert-chain-good.pem",
			},
			token:         "abcdef",
			expectedErr:   "Nil configuration passed",
			tokenFetchErr: "Nil configuration passed",
		},
		"Token fetch error": {
			cfg: &ClientConfig{
				RootCACertFile: "testdata/cert-chain-good.pem",
			},
			token:         "",
			expectedErr:   "Nil configuration passed",
			tokenFetchErr: "Nil configuration passed",
		},
		"Root certificate file read error": {
			cfg: &ClientConfig{
				RootCACertFile: "testdata/cert-chain-good_not_exist.pem",
			},
			token:         token,
			tokenFetchErr: "",
			expectedErr:   "open testdata/cert-chain-good_not_exist.pem: no such file or directory",
		},
		"Token fetched": {
			cfg: &ClientConfig{
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
		gcp := GcpClientImpl{&mockTokenFetcher{c.token, c.tokenFetchErr}}

		options, err := gcp.GetDialOptions(c.cfg)
		if len(c.expectedErr) > 0 {
			if err == nil {
				t.Errorf("%s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != c.expectedErr {
				t.Errorf("%s: incorrect error message: %s VS %s",
					id, err.Error(), c.expectedErr)
			}
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
