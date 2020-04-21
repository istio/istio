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

package authenticate

import (
	"reflect"
	"testing"

	"k8s.io/client-go/kubernetes/fake"

	"istio.io/istio/pkg/jwt"

	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
)

func TestNewKubeJWTAuthenticator(t *testing.T) {
	trustDomain := "testdomain.com"
	jwtPolicy := jwt.JWTPolicyThirdPartyJWT

	authenticator := NewKubeJWTAuthenticator(nil, "kubernetes", nil, trustDomain, jwtPolicy)
	expectedAuthenticator := &KubeJWTAuthenticator{
		trustDomain: trustDomain,
		jwtPolicy:   jwtPolicy,
		clusterID:   "kubernetes",
	}
	if !reflect.DeepEqual(authenticator, expectedAuthenticator) {
		t.Errorf("Unexpected authentication result: want %v but got %v",
			expectedAuthenticator, authenticator)
	}
}

func TestAuthenticate(t *testing.T) {
	testCases := map[string]struct {
		metadata       metadata.MD
		jwtPolicy      string
		expectedID     string
		expectedErrMsg string
	}{
		"No bearer token": {
			metadata: metadata.MD{
				"clusterid": []string{"Kubernetes"},
				"authorization": []string{
					"Basic callername",
				},
			},
			expectedErrMsg: "target JWT extraction error: no bearer token exists in HTTP authorization header",
		},
		"Review error": {
			metadata: metadata.MD{
				"clusterid": []string{"Kubernetes"},
				"authorization": []string{
					"Basic callername",
					"Bearer bearer-token",
				},
			},
			expectedErrMsg: "failed to validate the JWT: invalid JWT policy: ",
		},
		"Wrong identity length": {
			metadata: metadata.MD{
				"clusterid": []string{"Kubernetes"},
				"authorization": []string{
					"Basic callername",
					"Bearer bearer-token",
				},
			},
			expectedErrMsg: "failed to validate the JWT: invalid JWT policy: ",
		},
		"token not authenticated": {
			metadata: metadata.MD{
				"clusterid": []string{"Kubernetes"},
				"authorization": []string{
					"Basic callername",
					"Bearer bearer-token",
				},
			},
			jwtPolicy:      jwt.JWTPolicyFirstPartyJWT,
			expectedErrMsg: "failed to validate the JWT: the token is not authenticated",
		},
	}

	for id, tc := range testCases {
		t.Run(id, func(t *testing.T) {
			ctx := context.Background()
			if tc.metadata != nil {
				ctx = metadata.NewIncomingContext(ctx, tc.metadata)
			}

			client := fake.NewSimpleClientset()
			authenticator := NewKubeJWTAuthenticator(client, "Kubernetes", nil, "example.com", tc.jwtPolicy)
			actualCaller, err := authenticator.Authenticate(ctx)
			if len(tc.expectedErrMsg) > 0 {
				if err == nil {
					t.Errorf("Case %s: Succeeded. Error expected: %v", id, err)
				} else if err.Error() != tc.expectedErrMsg {
					t.Errorf("Case %s: Incorrect error message: \n%s\nVS\n%s",
						id, err.Error(), tc.expectedErrMsg)
				}
				return
			} else if err != nil {
				t.Errorf("Case %s: Unexpected Error: %v", id, err)
				return
			}

			expectedCaller := &Caller{
				AuthSource: AuthSourceIDToken,
				Identities: []string{tc.expectedID},
			}

			if !reflect.DeepEqual(actualCaller, expectedCaller) {
				t.Errorf("Case %q: Unexpected token: want %v but got %v", id, expectedCaller, actualCaller)
			}
		})
	}
}
