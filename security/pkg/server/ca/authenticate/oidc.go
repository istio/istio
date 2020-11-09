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
	"context"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc"
)

const (
	IDTokenAuthenticatorType = "IDTokenAuthenticator"
)

type JwtAuthenticator struct {
	provider    *oidc.Provider
	verifier    *oidc.IDTokenVerifier
	trustDomain string
}

var _ Authenticator = &JwtAuthenticator{}

// newJwtAuthenticator is used when running istiod outside of a cluster, to validate the tokens using OIDC
// K8S is created with --service-account-issuer, service-account-signing-key-file and service-account-api-audiences
// which enable OIDC.
func NewJwtAuthenticator(iss string, trustDomain, audience string) (*JwtAuthenticator, error) {
	provider, err := oidc.NewProvider(context.Background(), iss)
	if err != nil {
		return nil, fmt.Errorf("running in cluster with K8S tokens, but failed to initialize %s %s", iss, err)
	}

	return &JwtAuthenticator{
		trustDomain: trustDomain,
		provider:    provider,
		verifier:    provider.Verifier(&oidc.Config{ClientID: audience}),
	}, nil
}

// Authenticate - based on the old OIDC authenticator for mesh expansion.
func (j *JwtAuthenticator) Authenticate(ctx context.Context) (*Caller, error) {
	bearerToken, err := extractBearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("ID token extraction error: %v", err)
	}

	idToken, err := j.verifier.Verify(context.Background(), bearerToken)
	if err != nil {
		return nil, fmt.Errorf("failed to verify the ID token (error %v)", err)
	}

	// for GCP-issued JWT, the service account is in the "email" field
	sa := &JwtPayload{}

	if err := idToken.Claims(&sa); err != nil {
		return nil, fmt.Errorf("failed to extract email field from ID token: %v", err)
	}
	if !strings.HasPrefix(sa.Sub, "system:serviceaccount") {
		return nil, fmt.Errorf("invalid sub %v", sa.Sub)
	}
	parts := strings.Split(sa.Sub, ":")
	ns := parts[2]
	ksa := parts[3]

	return &Caller{
		AuthSource: AuthSourceIDToken,
		Identities: []string{fmt.Sprintf(identityTemplate, j.trustDomain, ns, ksa)},
	}, nil
}

type JwtPayload struct {
	// Aud is the expected audience, defaults to istio-ca - but is based on istiod.yaml configuration.
	// If set to a different value - use the value defined by istiod.yaml. Env variable can
	// still override
	Aud []string `json:"aud"`

	// Exp is not currently used - we don't use the token for authn, just to determine k8s settings
	Exp int `json:"exp"`

	// Issuer - configured by K8S admin for projected tokens. Will be used to verify all tokens.
	Iss string `json:"iss"`

	Sub string `json:"sub"`
}

func (j JwtAuthenticator) AuthenticatorType() string {
	return IDTokenAuthenticatorType
}
