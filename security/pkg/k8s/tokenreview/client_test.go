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

package tokenreview

import (
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	k8sauth "k8s.io/api/authentication/v1"
)

func TestReview(t *testing.T) {
	handler := http.HandlerFunc(server)
	ch := make(chan *httptest.Server)
	go func() {
		httpServer := httptest.NewTLSServer(handler)
		ch <- httpServer
	}()
	server := <-ch
	defer server.Close()

	testCases := map[string]struct {
		path           string
		id             string
		expectedErrMsg string
	}{
		"Invalid server": {
			path:           "/invalid",
			expectedErrMsg: "unmarshal response body returns an error: invalid character 'w' looking for beginning of value",
		},
		"Error status": {
			path:           "/bad/status",
			expectedErrMsg: "the TokenReview server returns error status: bad_token_review_status",
		},
		"Unauthenticated": {
			path:           "/unauthenticated",
			expectedErrMsg: "the TokenReview server authentication failed",
		},
		"Not in serviceaccounts group": {
			path:           "/not/serviceaccount",
			expectedErrMsg: "the JWT is not a service account",
		},
		"Invalid username": {
			path:           "/invalid/username",
			expectedErrMsg: "invalid username field in the token review result: default:example-pod-sa",
		},
		"Valid server": {
			path:           "/valid",
			id:             "default:example-pod-sa",
			expectedErrMsg: "",
		},
	}

	for id, tc := range testCases {
		cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
		client := NewClient(server.URL+tc.path, cert, "Bearer abcdef")
		identity, err := client.Review("test_jwt")

		if len(tc.expectedErrMsg) > 0 {
			if err == nil {
				t.Errorf("Case %s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != tc.expectedErrMsg {
				t.Errorf("Case %s: Incorrect error message: %s VS (expected) %s",
					id, err.Error(), tc.expectedErrMsg)
			}
			continue
		} else if err != nil {
			t.Errorf("Case %s: Unexpected Error: %v", id, err)
			continue
		}
		if identity != tc.id {
			t.Errorf("Case %s: unmatched identity %s VS (expected) %s", id, identity, tc.id)
		}
	}
}

func server(w http.ResponseWriter, req *http.Request) {
	switch req.URL.Path {
	case "/invalid":
		w.Write([]byte("wrong server"))
		break
	case "/bad/status":
		tokenReview := &k8sauth.TokenReview{
			Status: k8sauth.TokenReviewStatus{
				Error: "bad_token_review_status",
			},
		}
		bodyBytes, _ := json.Marshal(tokenReview)
		w.Write(bodyBytes)
		break
	case "/unauthenticated":
		tokenReview := &k8sauth.TokenReview{
			Status: k8sauth.TokenReviewStatus{
				Authenticated: false,
				Error:         "",
			},
		}
		bodyBytes, _ := json.Marshal(tokenReview)
		w.Write(bodyBytes)
		break
	case "/not/serviceaccount":
		tokenReview := &k8sauth.TokenReview{
			Status: k8sauth.TokenReviewStatus{
				Authenticated: true,
				User: k8sauth.UserInfo{
					Username: "system:serviceaccount:default:example-pod-sa",
					UID:      "ff578a9e-65d3-11e8-aad2-42010a8a001d",
					Groups:   []string{"system:authenticated"},
				},
				Error: "",
			},
		}
		bodyBytes, _ := json.Marshal(tokenReview)
		w.Write(bodyBytes)
		break
	case "/invalid/username":
		tokenReview := &k8sauth.TokenReview{
			Status: k8sauth.TokenReviewStatus{
				Authenticated: true,
				User: k8sauth.UserInfo{
					Username: "default:example-pod-sa",
					UID:      "ff578a9e-65d3-11e8-aad2-42010a8a001d",
					Groups:   []string{"system:serviceaccounts", "system:serviceaccounts:default", "system:authenticated"},
				},
				Error: "",
			},
		}
		bodyBytes, _ := json.Marshal(tokenReview)
		w.Write(bodyBytes)
		break
	case "/valid":
		tokenReview := &k8sauth.TokenReview{
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
		bodyBytes, _ := json.Marshal(tokenReview)
		w.Write(bodyBytes)
		break
	}
}
