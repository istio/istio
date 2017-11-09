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
	"strings"

	oidc "github.com/coreos/go-oidc"
	"github.com/golang/glog"
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

type user struct {
	authSource authSource
	identities []string
}

type authenticator interface {
	authenticate(ctx context.Context) *user
}

// An authenticator that extracts identities from client certificate.
type clientCertAuthenticator struct{}

// authenticate extracts identities from presented client certificates. This
// method assumes that certificate chain has been properly validated before
// this method is called. In other words, this method does not do certificate
// chain validation itself.
func (cca *clientCertAuthenticator) authenticate(ctx context.Context) *user {
	peer, ok := peer.FromContext(ctx)
	if !ok {
		glog.Info("no client certificate is presented")
		return nil
	}

	if authType := peer.AuthInfo.AuthType(); authType != "tls" {
		glog.Warningf("unsupported auth type: %q", authType)
		return nil
	}

	tlsInfo := peer.AuthInfo.(credentials.TLSInfo)
	chains := tlsInfo.State.VerifiedChains
	if len(chains) == 0 || len(chains[0]) == 0 {
		glog.Warningf("no verified chain is found")
		return nil
	}

	ids := pki.ExtractIDs(chains[0][0].Extensions)
	return &user{
		authSource: authSourceClientCertificate,
		identities: ids,
	}
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

func (ja *idTokenAuthenticator) authenticate(ctx context.Context) *user {
	bearerToken := extractBearerToken(ctx)
	if bearerToken == "" {
		glog.Warning("no bearer token exists")

		return nil
	}

	idToken, err := ja.verifier.Verify(context.Background(), bearerToken)
	if err != nil {
		glog.Warningf("failed to verify the ID token (error %v)", err)

		return nil
	}

	return &user{
		authSource: authSourceIDToken,
		identities: []string{idToken.Subject},
	}
}

func extractBearerToken(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		glog.Warning("no metadata is attached")
		return ""
	}

	authHeader, exists := md[httpAuthHeader]
	if !exists {
		glog.Warning("no HTTP authorization header exists")
		return ""
	}

	for _, value := range authHeader {
		if strings.HasPrefix(value, bearerTokenPrefix) {
			return strings.TrimPrefix(value, bearerTokenPrefix)
		}
	}

	glog.Warning("no bearer token exists in HTTP authorization header")
	return ""
}
