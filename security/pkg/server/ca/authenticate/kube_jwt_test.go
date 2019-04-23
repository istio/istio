// Copyright 2018 Istio Authors
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

package authenticate

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"

	"istio.io/istio/security/pkg/k8s/tokenreview"
)

type mockTokenReviewClient struct {
	id  []string
	err error
}

func (c mockTokenReviewClient) ValidateK8sJwt(jwt string) ([]string, error) {
	if c.id != nil {
		return c.id, nil
	}
	return nil, c.err
}

func TestNewKubeJWTAuthenticator(t *testing.T) {
	tmpdir, _ := ioutil.TempDir("", "test_dir")
	validCACertPath := filepath.Join(tmpdir, "cacert")
	validJWTPath := filepath.Join(tmpdir, "jwt")
	caCertFileContent := []byte("CACERT")
	jwtFileContent := []byte("JWT")
	trustDomain := "testdomain.com"
	url := "https://server/url"
	if err := ioutil.WriteFile(validCACertPath, caCertFileContent, 0777); err != nil {
		t.Errorf("Failed to write to testing CA cert file: %v", err)
	}
	if err := ioutil.WriteFile(validJWTPath, jwtFileContent, 0777); err != nil {
		t.Errorf("Failed to write to testing JWT file: %v", err)
	}

	testCases := map[string]struct {
		caCertPath     string
		jwtPath        string
		expectedErrMsg string
	}{
		"Invalid CA cert path": {
			caCertPath:     "/invalid/path",
			jwtPath:        validJWTPath,
			expectedErrMsg: "failed to read the CA certificate of k8s API server: open /invalid/path: no such file or directory",
		},
		"Invalid JWT path": {
			caCertPath:     validCACertPath,
			jwtPath:        "/invalid/path",
			expectedErrMsg: "failed to read Citadel JWT: open /invalid/path: no such file or directory",
		},
		"Valid paths": {
			caCertPath:     validCACertPath,
			jwtPath:        validJWTPath,
			expectedErrMsg: "",
		},
	}

	for id, tc := range testCases {
		authenticator, err := NewKubeJWTAuthenticator(url, tc.caCertPath, tc.jwtPath, trustDomain)
		if len(tc.expectedErrMsg) > 0 {
			if err == nil {
				t.Errorf("Case %s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != tc.expectedErrMsg {
				t.Errorf("Case %s: Incorrect error message: want %s but got %s",
					id, tc.expectedErrMsg, err.Error())
			}
			continue
		} else if err != nil {
			t.Errorf("Case %s: Unexpected Error: %v", id, err)
		}
		expectedAuthenticator := &KubeJWTAuthenticator{
			client:      tokenreview.NewK8sSvcAcctAuthn(url, caCertFileContent, string(jwtFileContent)),
			trustDomain: trustDomain,
		}
		if !reflect.DeepEqual(authenticator, expectedAuthenticator) {
			t.Errorf("Case %q: Unexpected authentication result: want %v but got %v",
				id, expectedAuthenticator, authenticator)
		}
	}
}

func TestAuthenticate(t *testing.T) {
	testCases := map[string]struct {
		metadata       metadata.MD
		client         tokenReviewClient
		expectedID     string
		expectedErrMsg string
	}{
		"No bearer token": {
			metadata: metadata.MD{
				"random": []string{},
				"authorization": []string{
					"Basic callername",
				},
			},
			expectedErrMsg: "target JWT extraction error: no bearer token exists in HTTP authorization header",
		},
		"Review error": {
			metadata: metadata.MD{
				"random": []string{},
				"authorization": []string{
					"Basic callername",
					"Bearer bearer-token",
				},
			},
			client:         &mockTokenReviewClient{id: nil, err: fmt.Errorf("test error")},
			expectedErrMsg: "failed to validate the JWT: test error",
		},
		"Wrong identity length": {
			metadata: metadata.MD{
				"random": []string{},
				"authorization": []string{
					"Basic callername",
					"Bearer bearer-token",
				},
			},
			client:         &mockTokenReviewClient{id: []string{"foo"}, err: nil},
			expectedErrMsg: "failed to parse the JWT. Validation result length is not 2, but 1",
		},
		"Successful": {
			metadata: metadata.MD{
				"random": []string{},
				"authorization": []string{
					"Basic callername",
					"Bearer bearer-token",
				},
			},
			client:         &mockTokenReviewClient{id: []string{"foo", "bar"}, err: nil},
			expectedID:     "spiffe://example.com/ns/foo/sa/bar",
			expectedErrMsg: "",
		},
	}

	for id, tc := range testCases {
		ctx := context.Background()
		if tc.metadata != nil {
			ctx = metadata.NewIncomingContext(ctx, tc.metadata)
		}

		authenticator := &KubeJWTAuthenticator{client: tc.client, trustDomain: "example.com"}

		actualCaller, err := authenticator.Authenticate(ctx)
		if len(tc.expectedErrMsg) > 0 {
			if err == nil {
				t.Errorf("Case %s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != tc.expectedErrMsg {
				t.Errorf("Case %s: Incorrect error message: %s VS %s",
					id, err.Error(), tc.expectedErrMsg)
			}
			continue
		} else if err != nil {
			t.Errorf("Case %s: Unexpected Error: %v", id, err)
			continue
		}

		expectedCaller := &Caller{
			AuthSource: AuthSourceIDToken,
			Identities: []string{tc.expectedID},
		}

		if !reflect.DeepEqual(actualCaller, expectedCaller) {
			t.Errorf("Case %q: Unexpected token: want %v but got %v", id, expectedCaller, actualCaller)
		}
	}
}
