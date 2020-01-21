// Copyright 2019 Istio Authors
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

package tokenreview

import (
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"istio.io/istio/pkg/jwt"

	k8sauth "k8s.io/api/authentication/v1"
)

var (
	// testJwtPrefix to distinguish between test jwts and real jwts.
	testJwtPrefix = "test-jwt"
)

type mockAPIServer struct {
	httpServer    *httptest.Server
	apiPath       string
	reviewerToken string
}

type clientConfig struct {
	tlsCert       []byte
	reviewPath    string
	reviewerToken string
	jwt           string
}

func TestOnMockAPIServer(t *testing.T) {
	testCases := map[string]struct {
		cliConfig    clientConfig
		expectedCert []string
		expectedErr  string
	}{
		"Valid request": {
			cliConfig: clientConfig{jwt: getJwtFromFile("testdata/legacy-jwt.jwt", t), tlsCert: []byte{}, reviewPath: "review-path",
				reviewerToken: "fake-reviewer-token"},
			expectedErr: "",
		},
		"Valid request but using legacy jwt": {
			cliConfig: clientConfig{jwt: getJwtFromFile("testdata/legacy-jwt.jwt", t), tlsCert: []byte{}, reviewPath: "review-path",
				reviewerToken: "fake-reviewer-token"},
			expectedErr: "legacy JWTs are not allowed and the provided jwt is not trustworthy",
		},
		"Valid request with trustworthy jwt": {
			cliConfig: clientConfig{jwt: getJwtFromFile("testdata/trustworthy-jwt.jwt", t), tlsCert: []byte{}, reviewPath: "review-path",
				reviewerToken: "fake-reviewer-token"},
			expectedErr: "",
		},
		"Valid request without JWT": {
			cliConfig: clientConfig{tlsCert: []byte{}, reviewPath: "review-path",
				reviewerToken: "fake-reviewer-token"},
			expectedErr: "failed to check if jwt is trustworthy: jwt may be invalid",
		},
		"Invalid JWT": {
			cliConfig: clientConfig{jwt: ":", tlsCert: []byte{}, reviewPath: "review-path",
				reviewerToken: "fake-reviewer-token"},
			expectedErr: "failed to check if jwt is trustworthy: jwt may be invalid",
		},
		"Wrong review path": {
			cliConfig: clientConfig{jwt: getJwtFromFile("testdata/trustworthy-jwt.jwt", t), tlsCert: []byte{}, reviewPath: "wrong-review-path",
				reviewerToken: "fake-reviewer-token"},
			expectedCert: nil,
			expectedErr:  "the request is of an invalid path",
		},
		"No review path": {
			cliConfig: clientConfig{jwt: getJwtFromFile("testdata/trustworthy-jwt.jwt", t), tlsCert: []byte{},
				reviewerToken: "fake-reviewer-token"},
			expectedCert: nil,
			expectedErr:  "the request is of an invalid path",
		},
		"No reviewer token": {
			cliConfig:   clientConfig{jwt: getJwtFromFile("testdata/trustworthy-jwt.jwt", t), tlsCert: []byte{}, reviewPath: "review-path"},
			expectedErr: "invalid token",
		},
		"Wrong reviewer token": {
			cliConfig: clientConfig{jwt: getJwtFromFile("testdata/trustworthy-jwt.jwt", t), tlsCert: []byte{}, reviewPath: "review-path",
				reviewerToken: "wrong-reviewer-token"},
			expectedCert: nil,
			expectedErr:  "invalid token",
		},
	}

	ch := make(chan *mockAPIServer)
	go func() {
		// create a test Vault server
		server := newMockAPIServer(t, "/review-path", "Bearer fake-reviewer-token")
		ch <- server
	}()
	s := <-ch
	defer s.httpServer.Close()

	for id, tc := range testCases {
		tc.cliConfig.tlsCert = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.httpServer.Certificate().Raw})
		if tc.cliConfig.tlsCert == nil {
			t.Errorf("invalid TLS certificate")
		}

		authn := NewK8sSvcAcctAuthn(s.httpServer.URL+"/"+tc.cliConfig.reviewPath, tc.cliConfig.tlsCert,
			tc.cliConfig.reviewerToken)

		_, err := authn.ValidateK8sJwt(tc.cliConfig.jwt, jwt.JWTPolicyThirdPartyJWT)

		if err != nil {
			t.Logf("Error: %v", err.Error())
			if !strings.Contains(err.Error(), tc.expectedErr) {
				t.Errorf("Test case [%s]: error (%s) does not match expected error (%s)", id, err.Error(), tc.expectedErr)
			}
		} else {
			t.Logf("No error")
			if tc.expectedErr != "" {
				t.Errorf("Test case [%s]: expect error: %s but got no error", id, tc.expectedErr)
			}
		}
	}
}

// newMockAPIServer creates a mock k8s API server for testing purpose.
// apiPath: the path to call token review API
// reviewerToken: the token of the reviewer
func newMockAPIServer(t *testing.T, apiPath, reviewerToken string) *mockAPIServer {
	apiServer := &mockAPIServer{
		apiPath:       apiPath,
		reviewerToken: reviewerToken,
	}

	handler := http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		t.Logf("request: %+v", *req)
		t.Logf("request URL path: %v", req.URL.Path)
		switch req.URL.Path {
		case apiServer.apiPath:
			t.Logf("%v", req.URL)
			body, err := ioutil.ReadAll(req.Body)
			if err != nil {
				t.Logf("failed to read the request body: %v", err)
				simpleTokenReviewResp(resp, false, "failed to read the request body")
				return
			}

			saReq := saValidationRequest{}
			err = json.Unmarshal(body, &saReq)
			if err != nil {
				t.Logf("failed to parse the request body: %v", err)
				simpleTokenReviewResp(resp, false, "failed to parse the request body")
				return
			}

			t.Logf("saValidationRequest: %+v", saReq)
			if apiServer.reviewerToken != req.Header.Get("Authorization") {
				t.Logf("invalid token: %v", req.Header.Get("Authorization"))
				simpleTokenReviewResp(resp, false, "invalid token")
			} else {
				t.Logf("Valid token: %v", req.Header.Get("Authorization"))

				// If the test uses a real jwt, decode the header, payload, and signature.
				if !strings.HasPrefix(saReq.Spec.Token, testJwtPrefix) {
					if !isJwtDecodable(saReq.Spec.Token) {
						simpleTokenReviewResp(resp, false, "invalid JWT")
						return
					}
				}

				result := &k8sauth.TokenReview{
					Status: k8sauth.TokenReviewStatus{
						Authenticated: true,
						User: k8sauth.UserInfo{
							Username: "system:serviceaccount:default:example-pod-sa",
							UID:      "ff578a9e-65d3-11e8-aad2-42010a8a001d",
							Groups:   []string{"system:serviceaccounts", "system:serviceaccounts:default", "system:authenticated"},
						},
						Error: "",
					},
				}
				resultJSON, _ := json.Marshal(result)
				resp.Header().Set("Content-Type", "application/json")
				resp.Write(resultJSON)
			}

		default:
			t.Logf("The request contains invalid path: %v", req.URL.Path)
			simpleTokenReviewResp(resp, false, "the request is of an invalid path")
		}
	})

	apiServer.httpServer = httptest.NewTLSServer(handler)

	t.Logf("Serving API server at: %v", apiServer.httpServer.URL)

	return apiServer
}

// nolint: unparam
func simpleTokenReviewResp(resp http.ResponseWriter, status bool, errMsg string) {
	result := &k8sauth.TokenReview{
		Status: k8sauth.TokenReviewStatus{
			Authenticated: status,
			Error:         errMsg,
		},
	}
	resultJSON, _ := json.Marshal(result)
	resp.Header().Set("Content-Type", "application/json")
	resp.Write(resultJSON)
}

// isJwtDecodable returns whether or not a jwt can be decoded.
func isJwtDecodable(jwt string) bool {
	jwtSplit := strings.Split(jwt, ".")
	if len(jwtSplit) != 3 {
		return false
	}
	// Try decoding the header and payload.
	for i := 0; i < 2; i++ {
		_, err := base64.RawStdEncoding.DecodeString(jwtSplit[i])
		if err != nil {
			return false
		}
	}
	return true
}

func TestIsTrustworthyJwt(t *testing.T) {
	testCases := []struct {
		Name           string
		Jwt            string
		ExpectedResult bool
	}{
		{
			Name:           "legacy jwt",
			Jwt:            getJwtFromFile("testdata/legacy-jwt.jwt", t),
			ExpectedResult: false,
		},
		{
			Name:           "trustworthy jwt",
			Jwt:            getJwtFromFile("testdata/trustworthy-jwt.jwt", t),
			ExpectedResult: true,
		},
	}

	for _, tc := range testCases {
		isTrustworthyJwt, err := isTrustworthyJwt(tc.Jwt)
		if err != nil {
			t.Errorf("%s failed with error %v", tc.Name, err.Error())
		}
		if isTrustworthyJwt != tc.ExpectedResult {
			t.Errorf("%s failed. For ExpectedResult: want result %v, got %v\n", tc.Name, tc.ExpectedResult, isTrustworthyJwt)
		}
	}
}

func getJwtFromFile(filePath string, t *testing.T) string {
	jwt, err := ioutil.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read %q", filePath)
	}
	return string(jwt)
}
