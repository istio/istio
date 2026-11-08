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

package xds_test

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	k8sauth "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	credentials "istio.io/istio/pilot/pkg/credentials/kube"
	"istio.io/istio/pilot/pkg/model"
	xdspkg "istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/test"
	"istio.io/istio/security/pkg/server/ca/authenticate/kubeauth"
)

type remoteGetter map[cluster.ID]kubernetes.Interface

func (r remoteGetter) GetRemoteKubeClient(c cluster.ID) kubernetes.Interface { return r[c] }

func (r remoteGetter) ListClusters() []cluster.ID {
	out := []cluster.ID{}
	for k := range r {
		out = append(out, k)
	}
	return out
}

type headerCreds map[string]string

func (h headerCreds) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return h, nil
}
func (h headerCreds) RequireTransportSecurity() bool { return false }

// TestXDSClusterBinding checks that the CLUSTER_ID a proxy claims in node metadata must match the
// cluster its token was validated against, so it cannot read secrets from another cluster.
func TestXDSClusterBinding(t *testing.T) {
	const (
		clusterA = cluster.ID("cluster-a")
		clusterB = cluster.ID("cluster-b")
	)
	tokens := map[cluster.ID]string{clusterA: "token-a", clusterB: "token-b"}
	bKey := readFile(filepath.Join(certDir, "dns/key.pem"))
	bOnly := makeSecret("b-only-cert", map[string]string{
		credentials.GenericScrtCert: readFile(filepath.Join(certDir, "dns/cert-chain.pem")),
		credentials.GenericScrtKey:  bKey,
	})

	// bufconn is plaintext; authenticate like the TLS port does
	test.SetForTest(t, &security.AuthPlaintext, true)

	clients := map[cluster.ID]kube.Client{}
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		DefaultClusterName: clusterA,
		KubernetesObjectsByCluster: map[cluster.ID][]runtime.Object{
			clusterA: {},
			clusterB: {bOnly},
		},
		KubeClientBuilder: func(objects ...runtime.Object) kube.Client {
			c := kube.NewFakeClient(objects...)
			id := clusterA
			if len(objects) > 0 {
				id = clusterB
			}
			clients[id] = c
			fc := c.Kube().(*fake.Clientset)
			// the same gateway service account exists and can read its namespace's secrets in both clusters
			xds.DisableAuthorizationForSecret(fc)
			fc.PrependReactor("create", "tokenreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
				tr := &k8sauth.TokenReview{}
				if action.(k8stesting.CreateAction).GetObject().(*k8sauth.TokenReview).Spec.Token == tokens[id] {
					tr.Status.Authenticated = true
					tr.Status.User = k8sauth.UserInfo{
						Username: "system:serviceaccount:istio-system:istio-ingressgateway",
						Groups:   []string{"system:serviceaccounts"},
					}
				}
				return true, tr, nil
			})
			return c
		},
	})
	// the fake server starts remote clients before the credentials controller registers its informers
	clients[clusterB].RunAndWait(test.NewStop(t))
	s.Discovery.Authenticators = []security.Authenticator{
		kubeauth.NewKubeJWTAuthenticator(meshwatcher.NewTestWatcher(mesh.DefaultMeshConfig()),
			clients[clusterA].Kube(), clusterA, nil, remoteGetter{clusterB: clients[clusterB].Kube()}),
	}

	connect := func(token string, headerCluster, nodeCluster cluster.ID) *xdspkg.AdsTest {
		conn, err := grpc.NewClient("passthrough:///buffcon",
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithPerRPCCredentials(headerCreds{"authorization": "Bearer " + token, "clusterid": string(headerCluster)}),
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
				return s.BufListener.Dial()
			}))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return xdspkg.NewAdsTest(t, conn).
			WithID("router~1.1.1.1~gw.istio-system~istio-system.svc.cluster.local").
			WithType(v3.SecretType).
			WithTimeout(10 * time.Second).
			WithMetadata(model.NodeMetadata{ClusterID: nodeCluster, Namespace: "istio-system", ServiceAccount: "istio-ingressgateway"})
	}
	req := func() *discovery.DiscoveryRequest {
		return &discovery.DiscoveryRequest{ResourceNames: []string{"kubernetes://b-only-cert"}}
	}

	t.Run("token from A rejected by B", func(t *testing.T) {
		a := connect(tokens[clusterA], clusterB, clusterB)
		if code := status.Code(a.ExpectError(t)); code != codes.Unauthenticated {
			t.Fatalf("expected Unauthenticated, got %v", code)
		}
	})
	t.Run("proxy in A gets nothing from B", func(t *testing.T) {
		a := connect(tokens[clusterA], clusterA, clusterA)
		a.Request(t, req())
		a.ExpectNoResponse(t)
	})
	t.Run("proxy in A claiming B is rejected", func(t *testing.T) {
		a := connect(tokens[clusterA], clusterA, clusterB)
		a.Request(t, req())
		if code := status.Code(a.ExpectError(t)); code != codes.PermissionDenied {
			t.Fatalf("expected PermissionDenied, got %v", code)
		}
	})
	t.Run("proxy in B gets B secret", func(t *testing.T) {
		resp := connect(tokens[clusterB], clusterB, clusterB).RequestResponseAck(t, req())
		secrets := xdstest.ExtractTLSSecrets(t, resp.Resources)
		if len(secrets) != 1 || string(secrets["kubernetes://b-only-cert"].GetTlsCertificate().GetPrivateKey().GetInlineBytes()) != bKey {
			t.Fatalf("expected B secret, got %d secrets", len(secrets))
		}
	})
}
