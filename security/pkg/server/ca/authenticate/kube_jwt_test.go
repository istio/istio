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

package authenticate

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

	"istio.io/istio/pkg/jwt"
	"istio.io/istio/pkg/security"
)

func TestNewKubeJWTAuthenticator(t *testing.T) {
	trustDomain := "testdomain.com"
	jwtPolicy := jwt.PolicyThirdParty

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
func Test3p(t *testing.T) {
	// Real token from a cluster, from
	///var/run/secrets/kubernetes.io/serviceaccount/token
	t1p := "eyJhbGciOiJSUzI1NiIsImtpZCI6ImFrSDdOSy14VHBXcUVZUGRJMUdRRXQ3dFJFeXhIN3JsNHdHWjFmX2Z6Qk0ifQ.eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2UiOiJmb3J0aW8tYXNtIiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZWNyZXQubmFtZSI6ImRlZmF1bHQtdG9rZW4tdmo5cDkiLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlcnZpY2UtYWNjb3VudC5uYW1lIjoiZGVmYXVsdCIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50LnVpZCI6IjNmNWQ1YzRmLTBlMTYtNGMwYy05MzM5LTNkZjcwN2Q0N2UyYyIsInN1YiI6InN5c3RlbTpzZXJ2aWNlYWNjb3VudDpmb3J0aW8tYXNtOmRlZmF1bHQifQ.Z8ZdK4m75BcQUUXVjrQqKcWyCPPjlVHh_Q4Az6OUqQIQp4Nc5z-cGa5BUhzEFsdzO1VZgvJo17Kn5or-mW5cCGjTQJio2voV_mq66DyoFLM53OZ-drOzWrc1S7Ma_mq5SsTsduYwDPgq49gGx-1etGZ9qEiyzMw0f8XswmAO3wUuMqfKiLpnBnu3keIeHkYfW9jy7hZrnbkUhDqnapJrBqBQoGIM2s3MdkS0s1HOQYZbObNvqDDnjXEzZwKiQMrppo2VkzAOUBxUobgRtpPVK1_uQdCyU75p6qKjkyx5tLrCP6DaV92BJOEOKT82qUT7e_EIv44XqlPQMAgGMyUM9g" // nolint: lll
	// Real token from /var/run/secrets/tokens/istio-token
	t3p := "eyJhbGciOiJSUzI1NiIsImtpZCI6ImFrSDdOSy14VHBXcUVZUGRJMUdRRXQ3dFJFeXhIN3JsNHdHWjFmX2Z6Qk0ifQ.eyJhdWQiOlsiY29zdGluLWFzbTEuc3ZjLmlkLmdvb2ciXSwiZXhwIjoxNTk3NTQ2MTYyLCJpYXQiOjE1OTc1MDI5NjIsImlzcyI6Imh0dHBzOi8vY29udGFpbmVyLmdvb2dsZWFwaXMuY29tL3YxL3Byb2plY3RzL2Nvc3Rpbi1hc20xL2xvY2F0aW9ucy91cy1jZW50cmFsMS1jL2NsdXN0ZXJzL2JpZzEiLCJrdWJlcm5ldGVzLmlvIjp7Im5hbWVzcGFjZSI6ImZvcnRpby1hc20iLCJwb2QiOnsibmFtZSI6ImZvcnRpby04OWY2Y2ZkNmYtOXJqNjQiLCJ1aWQiOiJmNzY1NjE4YS01NzY2LTRiOTctYjVlYi00MmYxZDkwMzU5MzEifSwic2VydmljZWFjY291bnQiOnsibmFtZSI6ImRlZmF1bHQiLCJ1aWQiOiIzZjVkNWM0Zi0wZTE2LTRjMGMtOTMzOS0zZGY3MDdkNDdlMmMifX0sIm5iZiI6MTU5NzUwMjk2Miwic3ViIjoic3lzdGVtOnNlcnZpY2VhY2NvdW50OmZvcnRpby1hc206ZGVmYXVsdCJ9.SkeGuKMlCq9e9O8fG0sd2nzJH_6-_ccioBElUSXtGiz84Myvi_UeTXTlxOy3SCrlepHQtBXQ38CecixOawB8RQitaIf1TgaHEye_ASkQ7Ei91O7OKxGjYqAyYRlpf5S72njZ2hmZZUJUgWjwsUXXWcsCfNu3jbr81V8v0fzxg8um-3iID8DzmEpgEhrUvM9rIFj5HwWkzvZ6ZLwQ3q8shiP21vSVsQh-wjV3zoK3ylymW_1v8hQt-2XzB11q0Hsm0W1PgxKyuw0DN9wGoInKiWmU1hEg8Vwi-2O6nqHcTfJ8P-0pM56MdqH_HJI66Ql8sjSazAF_aXSAZf1-RuPosg" // nolint: lll

	for _, s := range []string{t3p, "InvalidToken"} {
		if isK8SUnbound(s) {
			t.Error("Expecting bound token, detected unbound ", s)
		}
	}
	if !isK8SUnbound(t1p) {
		t.Error("Expecting unbound, detected bound ", t1p)
	}
}

func TestAuthenticate(t *testing.T) {
	primaryCluster := "Kubernetes"
	remoteCluster := "remote"
	invlidToken := "invalid-token"

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
			expectedErrMsg: "failed to validate the JWT: the token is not authenticated",
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
			expectedID:     fmt.Sprintf(identityTemplate, "example.com", "default", "example-pod-sa"),
			expectedErrMsg: "",
		},
		"not found remote cluster fallback to primary cluster": {
			remoteCluster: false,
			token:         "bearer-token",
			metadata: metadata.MD{
				"clusterid": []string{"non-exist"},
				"authorization": []string{
					"Basic callername",
				},
			},
			jwtPolicy:      jwt.PolicyFirstParty,
			expectedID:     fmt.Sprintf(identityTemplate, "example.com", "default", "example-pod-sa"),
			expectedErrMsg: "",
		},
	}

	for id, tc := range testCases {
		t.Run(id, func(t *testing.T) {
			ctx := context.Background()
			if tc.metadata != nil {
				if tc.token != "" {
					token := bearerTokenPrefix + tc.token
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

			remoteKubeClientGetter := func(clusterID string) kubernetes.Interface {
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

			authenticator := NewKubeJWTAuthenticator(client, "Kubernetes", remoteKubeClientGetter, "example.com", tc.jwtPolicy)
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
