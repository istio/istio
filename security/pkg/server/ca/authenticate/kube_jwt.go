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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/k8s/tokenreview"
)

const (
	// identityTemplate is the SPIFFE format template of the identity.
	identityTemplate         = "spiffe://%s/ns/%s/sa/%s"
	KubeJWTAuthenticatorType = "KubeJWTAuthenticator"
)

// TODO: move this under pkg/k8s - it depends on k8s client library.
// The cert and OIDC are independent of k8s.

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

var _ Authenticator = &KubeJWTAuthenticator{}

type jwtPayload struct {
	// Aud is JWT token audience - used to identify 3p tokens.
	// It is empty for the default K8S tokens.
	Aud []string `json:"aud"`
}

// isK8SUnbound detects if the token is a K8S unbound token.
// It is a regular JWT with no audience and expiration, which can
// be exchanged with bound tokens with audience.
//
// This is used to determine if we check audience in the token.
// Clients should not use unbound tokens except in cases where
// bound tokens are not possible.
func isK8SUnbound(jwt string) bool {
	jwtSplit := strings.Split(jwt, ".")
	if len(jwtSplit) != 3 {
		return false // unbound tokens are valid JWT
	}
	payload := jwtSplit[1]

	payloadBytes, err := base64.RawStdEncoding.DecodeString(payload)
	if err != nil {
		return false // unbound tokens are valid JWT
	}

	structuredPayload := &jwtPayload{}
	err = json.Unmarshal(payloadBytes, &structuredPayload)
	if err != nil {
		return false
	}

	return len(structuredPayload.Aud) == 0
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
	var aud []string

	// If the token has audience - we will validate it by setting in in the audiences field,
	// This happens regardless of Require3PToken setting.
	//
	// If 'Require3PToken' is set - we will also set the audiences field, forcing the check.
	// If Require3P is not set - and token does not have audience - we will
	// tolerate the unbound tokens.
	if !isK8SUnbound(targetJWT) || security.Require3PToken.Get() {
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
	if a.remoteKubeClientGetter != nil {
		if res := a.remoteKubeClientGetter(clusterID); res != nil {
			return res
		}
	}

	// failover to local cluster
	return a.kubeClient
}

func extractBearerToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", fmt.Errorf("no metadata is attached")
	}

	authHeader, exists := md[authorizationMeta]
	if !exists {
		return "", fmt.Errorf("no HTTP authorization header exists")
	}

	for _, value := range authHeader {
		if strings.HasPrefix(value, bearerTokenPrefix) {
			return strings.TrimPrefix(value, bearerTokenPrefix), nil
		}
	}

	return "", fmt.Errorf("no bearer token exists in HTTP authorization header")
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
