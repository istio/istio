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
	"net/http"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"

	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/pki/util"
)

const (
	ClientCertAuthenticatorType = "ClientCertAuthenticator"
)

// ClientCertAuthenticator extracts identities from client certificate.
type ClientCertAuthenticator struct{}

var _ security.Authenticator = &ClientCertAuthenticator{}

func (cca *ClientCertAuthenticator) AuthenticatorType() string {
	return ClientCertAuthenticatorType
}

// Authenticate extracts identities from presented client certificates. This
// method assumes that certificate chain has been properly validated before
// this method is called. In other words, this method does not do certificate
// chain validation itself.
func (cca *ClientCertAuthenticator) Authenticate(authCtx security.AuthContext) (*security.Caller, error) {
	if authCtx.GrpcContext != nil {
		return cca.authenticateGrpc(authCtx.GrpcContext)
	}
	if authCtx.Request != nil {
		return cca.authenticateHTTP(authCtx.Request)
	}
	return nil, nil
}

func (cca *ClientCertAuthenticator) authenticateGrpc(ctx context.Context) (*security.Caller, error) {
	peer, ok := peer.FromContext(ctx)
	if !ok || peer.AuthInfo == nil {
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

	ids, err := util.ExtractIDs(chains[0][0].Extensions)
	if err != nil {
		return nil, err
	}

	return &security.Caller{
		AuthSource: security.AuthSourceClientCertificate,
		Identities: ids,
	}, nil
}

// authenticateHTTP performs mTLS authentication for http requests. Requires having the endpoints on a listener
// with proper TLS configuration.
func (cca *ClientCertAuthenticator) authenticateHTTP(req *http.Request) (*security.Caller, error) {
	if req.TLS == nil || req.TLS.VerifiedChains == nil {
		return nil, fmt.Errorf("no client certificate is presented")
	}

	chains := req.TLS.VerifiedChains
	if len(chains) == 0 || len(chains[0]) == 0 {
		return nil, fmt.Errorf("no verified chain is found")
	}

	ids, err := util.ExtractIDs(chains[0][0].Extensions)
	if err != nil {
		return nil, err
	}

	return &security.Caller{
		AuthSource: security.AuthSourceClientCertificate,
		Identities: ids,
	}, nil
}
