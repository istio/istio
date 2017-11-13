// Copyright 2017 Istio Authors
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

package grpc

import (
	"fmt"
	"strings"

	oidc "github.com/coreos/go-oidc"
	"golang.org/x/net/context"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	"istio.io/istio/security/pkg/pki"
)

const (
	bearerTokenPrefix = "Bearer "
	httpAuthHeader    = "authorization"
	idTokenIssuer     = "https://accounts.google.com"
)

// authSource represents where authentication result is derived from.
type authSource int

const (
	authSourceClientCertificate authSource = iota
	authSourceIDToken
)

type caller struct {
	authSource authSource
	identities []string
}

type authenticator interface {
	authenticate(ctx context.Context) (*caller, error)
}

// An authenticator that extracts identities from client certificate.
type clientCertAuthenticator struct{}

// authenticate extracts identities from presented client certificates. This
// method assumes that certificate chain has been properly validated before
// this method is called. In other words, this method does not do certificate
// chain validation itself.
func (cca *clientCertAuthenticator) authenticate(ctx context.Context) (*caller, error) {
	peer, ok := peer.FromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("no client certificate is presented")
	}

	if authType := peer.AuthInfo.AuthType(); authType != "tls" {
		return nil, fmt.Errorf("unsupported auth type: %q", authType)

	}

	tlsInfo := peer.AuthInfo.(credentials.TLSInfo)
	chains := tlsInfo.State.VerifiedChains
	if len(chains) == 0 || len(chains[0]) == 0 {
		return nil, fmt.Errorf("no verified chain is found")
	}

	ids, err := pki.ExtractIDs(chains[0][0].Extensions)
	if err != nil {
		return nil, err
	}

	return &caller{
		authSource: authSourceClientCertificate,
		identities: ids,
	}, nil
}

// An authenticator that extracts identity from JWT. The JWT is requied to be
// transmitted using the "Bearer" authencitation scheme.
type idTokenAuthenticator struct {
	verifier *oidc.IDTokenVerifier
}

func newIDTokenAuthenticator(aud string) (*idTokenAuthenticator, error) {
	provider, err := oidc.NewProvider(context.Background(), idTokenIssuer)
	if err != nil {
		return nil, err
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: aud})
	return &idTokenAuthenticator{verifier}, nil
}

func (ja *idTokenAuthenticator) authenticate(ctx context.Context) (*caller, error) {
	bearerToken, err := extractBearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("ID token extraction error: %v", err)
	}

	idToken, err := ja.verifier.Verify(context.Background(), bearerToken)
	if err != nil {
		return nil, fmt.Errorf("failed to verify the ID token (error %v)", err)
	}

	return &caller{
		authSource: authSourceIDToken,
		identities: []string{idToken.Subject},
	}, nil
}

func extractBearerToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", fmt.Errorf("no metadata is attached")
	}

	authHeader, exists := md[httpAuthHeader]
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
