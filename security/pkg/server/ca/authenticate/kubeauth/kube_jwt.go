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

	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/k8s/tokenreview"
	"istio.io/istio/security/pkg/server/ca/authenticate"
	"istio.io/istio/security/pkg/util"
)

const (
	KubeJWTAuthenticatorType = "KubeJWTAuthenticator"

	clusterIDMeta = "clusterid"
)

type RemoteKubeClientGetter func(clusterID string) kubernetes.Interface

// KubeJWTAuthenticator authenticates K8s JWTs.
type KubeJWTAuthenticator struct {
	trustDomain string
	jwtPolicy   string

	// Primary cluster kube client
	kubeClient kubernetes.Interface
	// Primary cluster ID
	clusterID string

	// remote cluster kubeClient getter
	remoteKubeClientGetter RemoteKubeClientGetter
}

var _ security.Authenticator = &KubeJWTAuthenticator{}

// NewKubeJWTAuthenticator creates a new kubeJWTAuthenticator.
func NewKubeJWTAuthenticator(client kubernetes.Interface, clusterID string,
	remoteKubeClientGetter RemoteKubeClientGetter,
	trustDomain, jwtPolicy string) *KubeJWTAuthenticator {
	return &KubeJWTAuthenticator{
		trustDomain:            trustDomain,
		jwtPolicy:              jwtPolicy,
		kubeClient:             client,
		clusterID:              clusterID,
		remoteKubeClientGetter: remoteKubeClientGetter,
	}
}

func (a *KubeJWTAuthenticator) AuthenticatorType() string {
	return KubeJWTAuthenticatorType
}

// Authenticate authenticates the call using the K8s JWT from the context.
// The returned Caller.Identities is in SPIFFE format.
func (a *KubeJWTAuthenticator) Authenticate(ctx context.Context) (*security.Caller, error) {
	targetJWT, err := security.ExtractBearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("target JWT extraction error: %v", err)
	}
	clusterID := extractClusterID(ctx)
	var id []string

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
		// TODO: check the audience from token, no need to call
		// apiserver if audience is not matching. This may also
		// handle older apiservers that don't check audience.
	} else {
		// No audience will be passed to the check if the token
		// is unbound and the setting to require bound tokens is off
		aud = nil
	}
	id, err = tokenreview.ValidateK8sJwt(kubeClient, targetJWT, aud)
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
		Identities: []string{fmt.Sprintf(authenticate.IdentityTemplate, a.trustDomain, callerNamespace, callerServiceAccount)},
	}, nil
}

func (a *KubeJWTAuthenticator) GetKubeClient(clusterID string) kubernetes.Interface {
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

func extractClusterID(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}

	clusterIDHeader, exists := md[clusterIDMeta]
	if !exists {
		return ""
	}

	if len(clusterIDHeader) == 1 {
		return clusterIDHeader[0]
	}
	return ""
}
