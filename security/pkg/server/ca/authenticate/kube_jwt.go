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
	"fmt"

	"golang.org/x/net/context"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/security/pkg/k8s/tokenreview"
)

const (
	// identityTemplate is the SPIFFE format template of the identity.
	identityTemplate         = "spiffe://%s/ns/%s/sa/%s"
	KubeJWTAuthenticatorType = "KubeJWTAuthenticator"
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
func (a *KubeJWTAuthenticator) Authenticate(ctx context.Context) (*Caller, error) {
	targetJWT, err := extractBearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("target JWT extraction error: %v", err)
	}
	clusterID := extractClusterID(ctx)
	var id []string

	kubeClient := a.GetKubeClient(clusterID)
	if kubeClient == nil {
		return nil, fmt.Errorf("could not get cluster %s's kube client", clusterID)
	}
	id, err = tokenreview.ValidateK8sJwt(kubeClient, targetJWT, a.jwtPolicy)
	if err != nil {
		return nil, fmt.Errorf("failed to validate the JWT: %v", err)
	}
	if len(id) != 2 {
		return nil, fmt.Errorf("failed to parse the JWT. Validation result length is not 2, but %d", len(id))
	}
	callerNamespace := id[0]
	callerServiceAccount := id[1]
	return &Caller{
		AuthSource: AuthSourceIDToken,
		Identities: []string{fmt.Sprintf(identityTemplate, a.trustDomain, callerNamespace, callerServiceAccount)},
	}, nil
}

func (a *KubeJWTAuthenticator) GetKubeClient(clusterID string) kubernetes.Interface {
	// first match local/primary cluster
	if a.clusterID == clusterID {
		return a.kubeClient
	}

	// secondly try other remote clusters
	if res := a.remoteKubeClientGetter(clusterID); res != nil {
		return res
	}

	// failover to local cluster
	return a.kubeClient
}
