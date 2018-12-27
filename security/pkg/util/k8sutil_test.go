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

package util

import (
	"encoding/json"
	"encoding/pem"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	k8sauth "k8s.io/api/authentication/v1"
)

type mockAPIServer struct {
	httpServer *httptest.Server
	apiPath    string
	token      string
}

type clientConfig struct {
	tlsCert     []byte
	reviewPath  string
	clientToken string
}

func TestOnMockAPIServer(t *testing.T) {
	testCases := map[string]struct {
		cliConfig    clientConfig
		expectedCert []string
		expectedErr  string
	}{
		"Valid request": {
			cliConfig:   clientConfig{tlsCert: []byte{}, reviewPath: "review-path", clientToken: "Bearer fake-client-token"},
			expectedErr: "",
		},
		"Wrong review path": {
			cliConfig:    clientConfig{tlsCert: []byte{}, reviewPath: "wrong-review-path", clientToken: "Bearer fake-client-token"},
			expectedCert: nil,
			expectedErr:  "the service account authentication returns an error",
		},
		"Wrong client token": {
			cliConfig:    clientConfig{tlsCert: []byte{}, reviewPath: "review-path", clientToken: "wrong-client-token"},
			expectedCert: nil,
			expectedErr:  "the service account authentication returns an error",
		},
	}

	ch := make(chan *mockAPIServer)
	go func() {
		// create a test Vault server
		server := newMockAPIServer(t, "/review-path", "Bearer fake-client-token")
		ch <- server
	}()
	s := <-ch
	defer s.httpServer.Close()

	for id, tc := range testCases {
		tc.cliConfig.tlsCert = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.httpServer.Certificate().Raw})
		if tc.cliConfig.tlsCert == nil {
			t.Errorf("invalid TLS certificate")
		}

		_, err := ValidateK8sJwt(s.httpServer.URL+"/"+tc.cliConfig.reviewPath, tc.cliConfig.tlsCert, tc.cliConfig.clientToken, []byte{})

		if err != nil {
			match, _ := regexp.MatchString(tc.expectedErr+".+", err.Error())
			if !match {
				t.Errorf("Test case [%s]: error (%s) does not match expected error (%s)", id, err.Error(), tc.expectedErr)
			}
		} else {
			if tc.expectedErr != "" {
				t.Errorf("Test case [%s]: expect error: %s but got no error", id, tc.expectedErr)
			}
		}
	}
}

// newMockAPIServer creates a mock k8s API server for testing purpose.
// apiPath: the path to call token review API
// token: required access token
func newMockAPIServer(t *testing.T, apiPath, token string) *mockAPIServer {
	apiServer := &mockAPIServer{
		apiPath: apiPath,
		token:   token,
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
				result := &k8sauth.TokenReview{
					Status: k8sauth.TokenReviewStatus{
						Authenticated: false,
						Error:         "failed to read the request body",
					},
				}
				resultJSON, _ := json.Marshal(result)
				resp.Header().Set("Content-Type", "application/json")
				resp.Write(resultJSON)
				return
			}
			saReq := saValidationRequest{}
			err = json.Unmarshal(body, &saReq)
			if err != nil {
				t.Logf("failed to parse the request body: %v", err)
				result := &k8sauth.TokenReview{
					Status: k8sauth.TokenReviewStatus{
						Authenticated: false,
						Error:         "failed to parse the request body",
					},
				}
				resultJSON, _ := json.Marshal(result)
				resp.Header().Set("Content-Type", "application/json")
				resp.Write(resultJSON)
				return
			}
			t.Logf("saValidationRequest: %+v", saReq)
			if apiServer.token != req.Header.Get("Authorization") {
				t.Logf("invalid token: %v", req.Header.Get("Authorization"))
				result := &k8sauth.TokenReview{
					Status: k8sauth.TokenReviewStatus{
						Authenticated: false,
						Error:         "invalid token",
					},
				}
				resultJSON, _ := json.Marshal(result)
				resp.Header().Set("Content-Type", "application/json")
				resp.Write(resultJSON)
			} else {
				t.Logf("Valid token: %v", req.Header.Get("Authorization"))
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
			break
		default:
			t.Logf("The request contains invalid path: %v", req.URL.Path)
			result := &k8sauth.TokenReview{
				Status: k8sauth.TokenReviewStatus{
					Authenticated: false,
					Error:         "the request is of an invalid path",
				},
			}
			resultJSON, _ := json.Marshal(result)
			resp.Header().Set("Content-Type", "application/json")
			resp.Write(resultJSON)
		}
	})

	apiServer.httpServer = httptest.NewTLSServer(handler)

	t.Logf("Serving API server at: %v", apiServer.httpServer.URL)

	return apiServer
}
