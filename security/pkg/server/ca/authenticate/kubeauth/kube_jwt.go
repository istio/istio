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
	"net/http"
	"strings"

	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/jwt"
	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/k8s/tokenreview"
	"istio.io/istio/security/pkg/server/ca/authenticate"
	"istio.io/istio/security/pkg/util"
	"istio.io/pkg/log"
)

const (
	KubeJWTAuthenticatorType = "KubeJWTAuthenticator"

	clusterIDMeta = "clusterid"
)

type RemoteKubeClientGetter func(clusterID cluster.ID) kubernetes.Interface

// KubeJWTAuthenticator authenticates K8s JWTs.
type KubeJWTAuthenticator struct {
	// holder of a mesh configuration for dynamically updating trust domain
	meshHolder mesh.Holder

	jwtPolicy string

	// Primary cluster kube client
	kubeClient kubernetes.Interface
	// Primary cluster ID
	clusterID cluster.ID

	// remote cluster kubeClient getter
	remoteKubeClientGetter RemoteKubeClientGetter
}

var _ security.Authenticator = &KubeJWTAuthenticator{}

// NewKubeJWTAuthenticator creates a new kubeJWTAuthenticator.
func NewKubeJWTAuthenticator(meshHolder mesh.Holder, client kubernetes.Interface, clusterID cluster.ID,
	remoteKubeClientGetter RemoteKubeClientGetter, jwtPolicy string,
) *KubeJWTAuthenticator {
	return &KubeJWTAuthenticator{
		meshHolder:             meshHolder,
		jwtPolicy:              jwtPolicy,
		kubeClient:             client,
		clusterID:              clusterID,
		remoteKubeClientGetter: remoteKubeClientGetter,
	}
}

func (a *KubeJWTAuthenticator) AuthenticatorType() string {
	return KubeJWTAuthenticatorType
}

func isAllowedKubernetesAudience(a string) bool {
	// We do not use url.Parse() as it *requires* the protocol.
	a = strings.TrimPrefix(a, "https://")
	a = strings.TrimPrefix(a, "http://")
	return strings.HasPrefix(a, "kubernetes.default.svc")
}

func (a *KubeJWTAuthenticator) AuthenticateRequest(req *http.Request) (*security.Caller, error) {
	targetJWT, err := security.ExtractRequestToken(req)
	if err != nil {
		return nil, fmt.Errorf("target JWT extraction error: %v", err)
	}
	clusterID := cluster.ID(req.Header.Get(clusterIDMeta))
	return a.authenticate(targetJWT, clusterID)
}

// Authenticate authenticates the call using the K8s JWT from the context.
// The returned Caller.Identities is in SPIFFE format.
func (a *KubeJWTAuthenticator) Authenticate(ctx context.Context) (*security.Caller, error) {
	targetJWT, err := security.ExtractBearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("target JWT extraction error: %v", err)
	}
	clusterID := extractClusterID(ctx)

	return a.authenticate(targetJWT, clusterID)
}

func (a *KubeJWTAuthenticator) authenticate(targetJWT string, clusterID cluster.ID) (*security.Caller, error) {
	kubeClient := a.GetKubeClient(clusterID)
	if kubeClient == nil {
		return nil, fmt.Errorf("could not get cluster %s's kube client", clusterID)
	}
	var aud []string

	// If the token has audience - we will validate it by setting in in the audiences field,
	// This happens regardless of Require3PToken setting.
	//
	// If 'Require3PToken' is set - we will also set the audiences field, forcing the check.
	// If Require3P is not set - and token does not have audience - we will
	// tolerate the unbound tokens.
	if !util.IsK8SUnbound(targetJWT) || security.Require3PToken.Get() {
		aud = security.TokenAudiences
		if tokenAud, _ := util.ExtractJwtAud(targetJWT); len(tokenAud) == 1 && isAllowedKubernetesAudience(tokenAud[0]) {
			if a.jwtPolicy == jwt.PolicyFirstParty && !security.Require3PToken.Get() {
				// For backwards compatibility, if first-party-jwt is used and they don't require 3p, allow it but warn
				// This is intended to support first-party-jwt on Kubernetes 1.21+, where BoundServiceAccountTokenVolume
				// became default and started setting an audience to one of defaultAllowedKubernetesAudiences.
				// Users should disable first-party-jwt, but we don't want to break them on upgrade
				log.Warnf("Insecure first-party-jwt option used to validate token; use third-party-jwt")
				aud = nil
			} else {
				log.Warnf("Received token with aud %q, but expected 'kubernetes.default.svc'. BoundServiceAccountTokenVolume, "+
					"default in Kubernetes 1.21+, is not compatible with first-party-jwt", aud)
			}
		}
		// TODO: check the audience from token, no need to call
		// apiserver if audience is not matching. This may also
		// handle older apiservers that don't check audience.
	} else {
		// No audience will be passed to the check if the token
		// is unbound and the setting to require bound tokens is off
		aud = nil
	}
	id, err := tokenreview.ValidateK8sJwt(kubeClient, targetJWT, aud)
	if err != nil {
		return nil, fmt.Errorf("failed to validate the JWT from cluster %q: %v", clusterID, err)
	}
	if len(id) != 2 {
		return nil, fmt.Errorf("failed to parse the JWT. Validation result length is not 2, but %d", len(id))
	}
	callerNamespace := id[0]
	callerServiceAccount := id[1]
	return &security.Caller{
		AuthSource: security.AuthSourceIDToken,
		Identities: []string{fmt.Sprintf(authenticate.IdentityTemplate, a.meshHolder.Mesh().GetTrustDomain(), callerNamespace, callerServiceAccount)},
	}, nil
}

func (a *KubeJWTAuthenticator) GetKubeClient(clusterID cluster.ID) kubernetes.Interface {
	// first match local/primary cluster
	// or if clusterID is not sent (we assume that its a single cluster)
	if a.clusterID == clusterID || clusterID == "" {
		return a.kubeClient
	}

	// secondly try other remote clusters
	if a.remoteKubeClientGetter != nil {
		if res := a.remoteKubeClientGetter(clusterID); res != nil {
			return res
		}
	}

	// we did not find the kube client for this cluster.
	// return nil so that logs will show that this cluster is not available in istiod
	return nil
}

func extractClusterID(ctx context.Context) cluster.ID {
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
