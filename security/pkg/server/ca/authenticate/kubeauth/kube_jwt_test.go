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

package kubeauth

import (
	"fmt"
	"reflect"
	"testing"

	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
	k8sauth "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/jwt"
	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/server/ca/authenticate"
)

type mockMeshConfigHolder struct {
	trustDomain string
}

func (mh mockMeshConfigHolder) Mesh() *meshconfig.MeshConfig {
	return &meshconfig.MeshConfig{
		TrustDomain: mh.trustDomain,
	}
}

func TestNewKubeJWTAuthenticator(t *testing.T) {
	meshHolder := mockMeshConfigHolder{"testdomain.com"}
	jwtPolicy := jwt.PolicyThirdParty

	authenticator := NewKubeJWTAuthenticator(meshHolder, nil, "kubernetes", nil, jwtPolicy)
	expectedAuthenticator := &KubeJWTAuthenticator{
		meshHolder: meshHolder,
		jwtPolicy:  jwtPolicy,
		clusterID:  "kubernetes",
	}
	if !reflect.DeepEqual(authenticator, expectedAuthenticator) {
		t.Errorf("Unexpected authentication result: want %v but got %v",
			expectedAuthenticator, authenticator)
	}
}

func TestAuthenticate(t *testing.T) {
	primaryCluster := "Kubernetes"
	remoteCluster := cluster.ID("remote")
	invlidToken := "invalid-token"
	meshHolder := mockMeshConfigHolder{"example.com"}

	testCases := map[string]struct {
		remoteCluster  bool
		metadata       metadata.MD
		token          string
		jwtPolicy      string
		expectedID     string
		expectedErrMsg string
	}{
		"No bearer token": {
			metadata: metadata.MD{
				"clusterid": []string{primaryCluster},
				"authorization": []string{
					"Basic callername",
				},
			},
			expectedErrMsg: "target JWT extraction error: no bearer token exists in HTTP authorization header",
		},
		"token not authenticated": {
			token: invlidToken,
			metadata: metadata.MD{
				"clusterid": []string{primaryCluster},
				"authorization": []string{
					"Basic callername",
				},
			},
			jwtPolicy:      jwt.PolicyFirstParty,
			expectedErrMsg: `failed to validate the JWT from cluster "Kubernetes": the token is not authenticated`,
		},
		"token authenticated": {
			token: "bearer-token",
			metadata: metadata.MD{
				"clusterid": []string{primaryCluster},
				"authorization": []string{
					"Basic callername",
				},
			},
			jwtPolicy:      jwt.PolicyFirstParty,
			expectedID:     fmt.Sprintf(authenticate.IdentityTemplate, "example.com", "default", "example-pod-sa"),
			expectedErrMsg: "",
		},
		"not found remote cluster results in error": {
			remoteCluster: false,
			token:         "bearer-token",
			metadata: metadata.MD{
				"clusterid": []string{"non-exist"},
				"authorization": []string{
					"Basic callername",
				},
			},
			jwtPolicy:      jwt.PolicyFirstParty,
			expectedErrMsg: "could not get cluster non-exist's kube client",
		},
	}

	for id, tc := range testCases {
		t.Run(id, func(t *testing.T) {
			ctx := context.Background()
			if tc.metadata != nil {
				if tc.token != "" {
					token := security.BearerTokenPrefix + tc.token
					tc.metadata.Append("authorization", token)
				}
				ctx = metadata.NewIncomingContext(ctx, tc.metadata)
			}

			tokenReview := &k8sauth.TokenReview{
				Spec: k8sauth.TokenReviewSpec{
					Token: tc.token,
				},
			}
			if tc.jwtPolicy == jwt.PolicyThirdParty {
				tokenReview.Spec.Audiences = security.TokenAudiences
			}

			tokenReview.Status.Audiences = []string{}
			if tc.token != invlidToken {
				tokenReview.Status.Authenticated = true
			}
			tokenReview.Status.User = k8sauth.UserInfo{
				Username: "system:serviceaccount:default:example-pod-sa",
				Groups:   []string{"system:serviceaccounts"},
			}

			client := fake.NewSimpleClientset()
			if !tc.remoteCluster {
				client.PrependReactor("create", "tokenreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
					return true, tokenReview, nil
				})
			}

			remoteKubeClientGetter := func(clusterID cluster.ID) kubernetes.Interface {
				if clusterID == remoteCluster {
					client := fake.NewSimpleClientset()
					if tc.remoteCluster {
						client.PrependReactor("create", "tokenreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
							return true, tokenReview, nil
						})
					}
				}
				return nil
			}

			authenticator := NewKubeJWTAuthenticator(meshHolder, client, "Kubernetes", remoteKubeClientGetter, tc.jwtPolicy)
			actualCaller, err := authenticator.Authenticate(security.AuthContext{GrpcContext: ctx})
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

			expectedCaller := &security.Caller{
				AuthSource: security.AuthSourceIDToken,
				Identities: []string{tc.expectedID},
			}

			if !reflect.DeepEqual(actualCaller, expectedCaller) {
				t.Errorf("Case %q: Unexpected token: want %v but got %v", id, expectedCaller, actualCaller)
			}
		})
	}
}

func TestIsAllowedKubernetesAudience(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"kubernetes.default.svc", true},
		{"kubernetes.default.svc.cluster.local", true},
		{"https://kubernetes.default.svc", true},
		{"https://kubernetes.default.svc.cluster.local", true},
		{"foo.default.svc", false},
		{"foo.default.svc:80", false},
		{"https://foo.default.svc:80", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := isAllowedKubernetesAudience(tt.in); got != tt.want {
				t.Errorf("isAllowedKubernetesAudience() = %v, want %v", got, tt.want)
			}
		})
	}
}
