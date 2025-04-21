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
	"context"
	"reflect"
	"testing"

	"google.golang.org/grpc/metadata"
	k8sauth "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/test/util/assert"
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
	authenticator := NewKubeJWTAuthenticator(meshHolder, nil, constants.DefaultClusterName, nil, nil)
	expectedAuthenticator := &KubeJWTAuthenticator{
		meshHolder: meshHolder,
		clusterID:  constants.DefaultClusterName,
	}
	if !reflect.DeepEqual(authenticator, expectedAuthenticator) {
		t.Errorf("Unexpected authentication result: want %v but got %v",
			expectedAuthenticator, authenticator)
	}
}

type fakeRemoteGetter struct {
	f func(clusterID cluster.ID) kubernetes.Interface
}

func (f fakeRemoteGetter) GetRemoteKubeClient(clusterID cluster.ID) kubernetes.Interface {
	return f.f(clusterID)
}

func (f fakeRemoteGetter) ListClusters() []cluster.ID {
	return []cluster.ID{"test-remote"}
}

var _ RemoteKubeClientGetter = fakeRemoteGetter{}

func TestAuthenticate(t *testing.T) {
	primaryCluster := constants.DefaultClusterName
	primaryClusterAlias := cluster.ID("alias")
	remoteCluster := cluster.ID("remote")
	invlidToken := "invalid-token"
	meshHolder := mockMeshConfigHolder{"example.com"}

	testCases := map[string]struct {
		remoteCluster  bool
		metadata       metadata.MD
		token          string
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
			expectedID:     spiffe.MustGenSpiffeURIForTrustDomain("example.com", "default", "example-pod-sa"),
			expectedErrMsg: "",
		},
		"token authenticated - cluster alias": {
			token: "bearer-token",
			metadata: metadata.MD{
				"clusterid": []string{primaryClusterAlias.String()},
				"authorization": []string{
					"Basic callername",
				},
			},
			expectedID:     spiffe.MustGenSpiffeURIForTrustDomain("example.com", "default", "example-pod-sa"),
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
			expectedErrMsg: `client claims to be in cluster "non-exist", but we only know about local cluster "Kubernetes" and remote clusters [test-remote]`,
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

			tokenReview.Status.Audiences = []string{}
			if tc.token != invlidToken {
				tokenReview.Status.Authenticated = true
			}
			tokenReview.Status.User = k8sauth.UserInfo{
				Username: "system:serviceaccount:default:example-pod-sa",
				Groups:   []string{"system:serviceaccounts"},
			}

			client := fake.NewClientset()
			if !tc.remoteCluster {
				client.PrependReactor("create", "tokenreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
					return true, tokenReview, nil
				})
			}

			remoteKubeClientGetter := func(clusterID cluster.ID) kubernetes.Interface {
				if clusterID == remoteCluster {
					client := fake.NewClientset()
					if tc.remoteCluster {
						client.PrependReactor("create", "tokenreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
							return true, tokenReview, nil
						})
					}
				}
				return nil
			}

			authenticator := NewKubeJWTAuthenticator(
				meshHolder,
				client,
				constants.DefaultClusterName,
				[]cluster.ID{primaryClusterAlias},
				fakeRemoteGetter{remoteKubeClientGetter})
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
				KubernetesInfo: security.KubernetesInfo{
					PodNamespace:      "default",
					PodServiceAccount: "example-pod-sa",
				},
			}

			assert.Equal(t, actualCaller, expectedCaller)
		})
	}
}
