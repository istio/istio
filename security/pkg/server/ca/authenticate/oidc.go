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
	"context"
	"fmt"
	"strings"

	oidc "github.com/coreos/go-oidc/v3/oidc"

	"istio.io/api/security/v1beta1"
	"istio.io/istio/pkg/security"
)

const (
	IDTokenAuthenticatorType = "IDTokenAuthenticator"
)

type JwtAuthenticator struct {
	trustDomain string
	audiences   []string
	verifier    *oidc.IDTokenVerifier
}

var _ security.Authenticator = &JwtAuthenticator{}

// newJwtAuthenticator is used when running istiod outside of a cluster, to validate the tokens using OIDC
// K8S is created with --service-account-issuer, service-account-signing-key-file and service-account-api-audiences
// which enable OIDC.
func NewJwtAuthenticator(jwtRule *v1beta1.JWTRule, trustDomain string) (*JwtAuthenticator, error) {
	issuer := jwtRule.GetIssuer()
	jwksURL := jwtRule.GetJwksUri()
	// The key of a JWT issuer may change, so the key may need to be updated.
	// Based on https://pkg.go.dev/github.com/coreos/go-oidc/v3/oidc#NewRemoteKeySet
	// the oidc library handles caching and cache invalidation. Thus, the verifier
	// is only created once in the constructor.
	var verifier *oidc.IDTokenVerifier
	if len(jwksURL) == 0 {
		// OIDC discovery is used if jwksURL is not set.
		provider, err := oidc.NewProvider(context.Background(), issuer)
		// OIDC discovery may fail, e.g. http request for the OIDC server may fail.
		if err != nil {
			return nil, fmt.Errorf("failed at creating an OIDC provider for %v: %v", issuer, err)
		}
		verifier = provider.Verifier(&oidc.Config{SkipClientIDCheck: true})
	} else {
		keySet := oidc.NewRemoteKeySet(context.Background(), jwksURL)
		verifier = oidc.NewVerifier(issuer, keySet, &oidc.Config{SkipClientIDCheck: true})
	}
	return &JwtAuthenticator{
		trustDomain: trustDomain,
		verifier:    verifier,
		audiences:   jwtRule.Audiences,
	}, nil
}

// Authenticate - based on the old OIDC authenticator for mesh expansion.
func (j *JwtAuthenticator) Authenticate(authRequest security.AuthContext) (*security.Caller, error) {
	if authRequest.GrpcContext != nil {
		bearerToken, err := security.ExtractBearerToken(authRequest.GrpcContext)
		if err != nil {
			return nil, fmt.Errorf("ID token extraction error: %v", err)
		}
		return j.authenticate(authRequest.GrpcContext, bearerToken)
	}
	if authRequest.Request != nil {
		bearerToken, err := security.ExtractRequestToken(authRequest.Request)
		if err != nil {
			return nil, fmt.Errorf("target JWT extraction error: %v", err)
		}
		return j.authenticate(authRequest.Request.Context(), bearerToken)
	}
	return nil, nil
}

func (j *JwtAuthenticator) authenticate(ctx context.Context, bearerToken string) (*security.Caller, error) {
	idToken, err := j.verifier.Verify(ctx, bearerToken)
	if err != nil {
		return nil, fmt.Errorf("failed to verify the JWT token (error %v)", err)
	}

	sa := JwtPayload{}
	// "aud" for trust domain, "sub" has "system:serviceaccount:$namespace:$serviceaccount".
	// in future trust domain may use another field as a standard is defined.
	if err := idToken.Claims(&sa); err != nil {
		return nil, fmt.Errorf("failed to extract claims from ID token: %v", err)
	}
	if !strings.HasPrefix(sa.Sub, "system:serviceaccount") {
		return nil, fmt.Errorf("invalid sub %v", sa.Sub)
	}
	parts := strings.Split(sa.Sub, ":")
	ns := parts[2]
	ksa := parts[3]
	if !checkAudience(sa.Aud, j.audiences) {
		return nil, fmt.Errorf("invalid audiences %v", sa.Aud)
	}

	return &security.Caller{
		AuthSource: security.AuthSourceIDToken,
		Identities: []string{fmt.Sprintf(IdentityTemplate, j.trustDomain, ns, ksa)},
	}, nil
}

// checkAudience() returns true if the audiences to check are in
// the expected audiences. Otherwise, return false.
func checkAudience(audToCheck []string, audExpected []string) bool {
	for _, a := range audToCheck {
		for _, b := range audExpected {
			if a == b {
				return true
			}
		}
	}
	return false
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
