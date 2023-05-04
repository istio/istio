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

package tokenreview

import (
	"fmt"
	"testing"

	authenticationv1 "k8s.io/api/authentication/v1"

	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/test/util/assert"
)

// TestGetTokenReviewResult verifies that getTokenReviewResult returns expected {<namespace>, <serviceaccountname>}.
func TestGetTokenReviewResult(t *testing.T) {
	testCases := []struct {
		name           string
		tokenReview    authenticationv1.TokenReview
		expectedError  error
		expectedResult security.KubernetesInfo
	}{
		{
			name: "the service account authentication error",
			tokenReview: authenticationv1.TokenReview{
				Status: authenticationv1.TokenReviewStatus{
					Error: "authentication error",
				},
			},
			expectedError: fmt.Errorf("the service account authentication returns an error: authentication error"),
		},
		{
			name: "not authenticated",
			tokenReview: authenticationv1.TokenReview{
				Status: authenticationv1.TokenReviewStatus{
					Authenticated: false,
				},
			},
			expectedError: fmt.Errorf("the token is not authenticated"),
		},
		{
			name: "token is not a service account",
			tokenReview: authenticationv1.TokenReview{
				Status: authenticationv1.TokenReviewStatus{
					Authenticated: true,
					User: authenticationv1.UserInfo{
						Groups: []string{
							"system:serviceaccounts:default",
						},
					},
				},
			},
			expectedError: fmt.Errorf("the token is not a service account"),
		},
		{
			name: "invalid username",
			tokenReview: authenticationv1.TokenReview{
				Status: authenticationv1.TokenReviewStatus{
					Authenticated: true,
					User: authenticationv1.UserInfo{
						Username: "system:serviceaccount:example-pod-sa",
						Groups: []string{
							"system:serviceaccounts",
							"system:serviceaccounts:default",
							"system:authenticated",
						},
					},
				},
			},
			expectedError: fmt.Errorf("invalid username field in the token review result"),
		},
		{
			name: "success",
			tokenReview: authenticationv1.TokenReview{
				Status: authenticationv1.TokenReviewStatus{
					Authenticated: true,
					User: authenticationv1.UserInfo{
						Username: "system:serviceaccount:default:example-pod-sa",
						UID:      "ff578a9e-65d3-11e8-aad2-42010a8a001d",
						Groups: []string{
							"system:serviceaccounts",
							"system:serviceaccounts:default",
							"system:authenticated",
						},
					},
				},
			},
			expectedResult: security.KubernetesInfo{
				PodNamespace:      "default",
				PodServiceAccount: "example-pod-sa",
			},
		},
		{
			name: " pod token",
			tokenReview: authenticationv1.TokenReview{
				Status: authenticationv1.TokenReviewStatus{
					Authenticated: true,
					User: authenticationv1.UserInfo{
						Username: "system:serviceaccount:default:example-pod-sa",
						UID:      "ff578a9e-65d3-11e8-aad2-42010a8a001d",
						Groups: []string{
							"system:serviceaccounts",
							"system:serviceaccounts:default",
							"system:authenticated",
						},
						Extra: map[string]authenticationv1.ExtraValue{
							PodNameKey: []string{"some-pod"},
							PodUIDKey:  []string{"12345"},
						},
					},
				},
			},
			expectedResult: security.KubernetesInfo{
				PodNamespace:      "default",
				PodServiceAccount: "example-pod-sa",
				PodUID:            "12345",
				PodName:           "some-pod",
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := getTokenReviewResult(&tc.tokenReview)
			assert.Equal(t, result, tc.expectedResult)
			assert.Equal(t, err, tc.expectedError)
		})
	}
}
