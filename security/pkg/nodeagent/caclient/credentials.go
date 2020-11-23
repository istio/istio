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

package caclient

import (
	"context"
	"fmt"
	"io/ioutil"
	"strings"

	"google.golang.org/grpc/credentials"

	"istio.io/istio/pkg/security"
)

type TokenProvider struct {
	opts security.Options
	// TokenProvider can be used for both XDS and CA connection. Because CA is often used with
	// external systems and XDS is not often (yet?), many of the security options only apply to CA
	// communication. A more proper solution would be to have separate options for CA and XDS, but
	// this requires API changes.
	forCA bool
}

var _ credentials.PerRPCCredentials = &TokenProvider{}

// TODO add metrics
// TODO change package
func NewCATokenProvider(opts security.Options) *TokenProvider {
	return &TokenProvider{opts, true}
}

func NewXDSTokenProvider(opts security.Options) *TokenProvider {
	return &TokenProvider{opts, false}
}

func (t *TokenProvider) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	if t == nil {
		return nil, nil
	}
	token, err := t.GetToken()
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, nil
	}
	return map[string]string{
		"authorization": "Bearer " + token,
	}, nil
}

func (t *TokenProvider) RequireTransportSecurity() bool {
	return false
}

func (t *TokenProvider) GetToken() (string, error) {
	if !t.forCA {
		if t.opts.JWTPath == "" {
			return "", nil
		}
		tok, err := ioutil.ReadFile(t.opts.JWTPath)
		if err != nil {
			return "", fmt.Errorf("fetch token from file: %v", err)
		}
		return strings.TrimSpace(string(tok)), nil
	}
	var token string
	if t.opts.CredFetcher != nil {
		var err error
		token, err = t.opts.CredFetcher.GetPlatformCredential()
		if err != nil {
			return "", fmt.Errorf("fetch platform credential: %v", err)
		}
	} else {
		if t.opts.JWTPath == "" {
			return "", nil
		}
		tok, err := ioutil.ReadFile(t.opts.JWTPath)
		if err != nil {
			return "", fmt.Errorf("fetch token from file: %v", err)
		}
		token = strings.TrimSpace(string(tok))
	}

	return t.exchangeToken(token)
}

func (t *TokenProvider) exchangeToken(token string) (string, error) {
	if t.opts.TokenExchanger == nil {
		return token, nil
	}
	return t.opts.TokenExchanger.ExchangeToken(t.opts.TrustDomain, token)
}
