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
	"fmt"
	"net/http"

	"google.golang.org/grpc/metadata"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/security/pkg/k8s/tokenreview"
)

const (
	KubeJWTAuthenticatorType = "KubeJWTAuthenticator"

	clusterIDMeta = "clusterid"
)

type RemoteKubeClientGetter interface {
	GetRemoteKubeClient(clusterID cluster.ID) kubernetes.Interface
	ListClusters() []cluster.ID
}

// KubeJWTAuthenticator authenticates K8s JWTs.
type KubeJWTAuthenticator struct {
	// holder of a mesh configuration for dynamically updating trust domain
	meshHolder mesh.Holder

	// Primary cluster kube client
	kubeClient kubernetes.Interface
	// Primary cluster ID
	clusterID cluster.ID
	// Primary cluster alisases
	clusterAliases map[cluster.ID]cluster.ID

	// remote cluster kubeClient getter
	remoteKubeClientGetter RemoteKubeClientGetter
}

var _ security.Authenticator = &KubeJWTAuthenticator{}

// NewKubeJWTAuthenticator creates a new kubeJWTAuthenticator.
func NewKubeJWTAuthenticator(
	meshHolder mesh.Holder,
	client kubernetes.Interface,
	clusterID cluster.ID,
	clusterAliases map[string]string,
	remoteKubeClientGetter RemoteKubeClientGetter,
) *KubeJWTAuthenticator {
	out := &KubeJWTAuthenticator{
		meshHolder:             meshHolder,
		kubeClient:             client,
		clusterID:              clusterID,
		remoteKubeClientGetter: remoteKubeClientGetter,
	}

	out.clusterAliases = make(map[cluster.ID]cluster.ID)
	for alias := range clusterAliases {
		out.clusterAliases[cluster.ID(alias)] = cluster.ID(clusterAliases[alias])
	}

	return out
}

func (a *KubeJWTAuthenticator) AuthenticatorType() string {
	return KubeJWTAuthenticatorType
}

// Authenticate authenticates the call using the K8s JWT from the context.
// The returned Caller.Identities is in SPIFFE format.
func (a *KubeJWTAuthenticator) Authenticate(authRequest security.AuthContext) (*security.Caller, error) {
	if authRequest.GrpcContext != nil {
		return a.authenticateGrpc(authRequest.GrpcContext)
	}
	if authRequest.Request != nil {
		return a.authenticateHTTP(authRequest.Request)
	}
	return nil, nil
}

func (a *KubeJWTAuthenticator) authenticateHTTP(req *http.Request) (*security.Caller, error) {
	targetJWT, err := security.ExtractRequestToken(req)
	if err != nil {
		return nil, fmt.Errorf("target JWT extraction error: %v", err)
	}
	clusterID := cluster.ID(req.Header.Get(clusterIDMeta))
	return a.authenticate(targetJWT, clusterID)
}

func (a *KubeJWTAuthenticator) authenticateGrpc(ctx context.Context) (*security.Caller, error) {
	targetJWT, err := security.ExtractBearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("target JWT extraction error: %v", err)
	}
	clusterID := ExtractClusterID(ctx)

	return a.authenticate(targetJWT, clusterID)
}

func (a *KubeJWTAuthenticator) authenticate(targetJWT string, clusterID cluster.ID) (*security.Caller, error) {
	kubeClient := a.getKubeClient(clusterID)
	if kubeClient == nil {
		return nil, fmt.Errorf("client claims to be in cluster %q, but we only know about local cluster %q and remote clusters %v",
			clusterID, a.clusterID, a.remoteKubeClientGetter.ListClusters())
	}

	id, err := tokenreview.ValidateK8sJwt(kubeClient, targetJWT, security.TokenAudiences)
	if err != nil {
		return nil, fmt.Errorf("failed to validate the JWT from cluster %q: %v", clusterID, err)
	}
	if id.PodServiceAccount == "" {
		return nil, fmt.Errorf("failed to parse the JWT; service account required")
	}
	if id.PodNamespace == "" {
		return nil, fmt.Errorf("failed to parse the JWT; namespace required")
	}
	return &security.Caller{
		AuthSource:     security.AuthSourceIDToken,
		Identities:     []string{spiffe.MustGenSpiffeURI(a.meshHolder.Mesh(), id.PodNamespace, id.PodServiceAccount)},
		KubernetesInfo: id,
	}, nil
}

func (a *KubeJWTAuthenticator) getKubeClient(clusterID cluster.ID) kubernetes.Interface {
	// first match local/primary cluster and it's aliases
	// or if clusterID is not sent (we assume that its a single cluster)
	if a.clusterID == clusterID || a.clusterID == a.clusterAliases[clusterID] || clusterID == "" {
		return a.kubeClient
	}

	// secondly try other remote clusters
	if a.remoteKubeClientGetter != nil {
		if res := a.remoteKubeClientGetter.GetRemoteKubeClient(clusterID); res != nil {
			return res
		}
		if res := a.remoteKubeClientGetter.GetRemoteKubeClient(a.clusterAliases[clusterID]); res != nil {
			return res
		}
	}

	// we did not find the kube client for this cluster.
	// return nil so that logs will show that this cluster is not available in istiod
	return nil
}

func ExtractClusterID(ctx context.Context) cluster.ID {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}

	clusterIDHeader, exists := md[clusterIDMeta]
	if !exists {
		return ""
	}

	if len(clusterIDHeader) == 1 {
		return cluster.ID(clusterIDHeader[0])
	}
	return ""
}
