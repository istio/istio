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

	"google.golang.org/grpc/credentials"

	"istio.io/istio/pkg/security"
)

// TokenProvider is a grpc PerRPCCredentials that can be used to attach a JWT token to each gRPC call.
// TokenProvider can be used for XDS, which may involve token exchange through STS.
type DefaultTokenProvider struct {
	opts *security.Options
}

var _ credentials.PerRPCCredentials = &DefaultTokenProvider{}

func NewDefaultTokenProvider(opts *security.Options) credentials.PerRPCCredentials {
	return &DefaultTokenProvider{opts}
}

func (t *DefaultTokenProvider) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
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

// Allow the token provider to be used regardless of transport security; callers can determine whether
// this is safe themselves.
func (t *DefaultTokenProvider) RequireTransportSecurity() bool {
	return false
}

// GetToken fetches a token to attach to a request. Returning "", nil will cause no header to be
// added; while a non-nil error will block the request If the token selected is not found, no error
// will be returned, causing no authorization header to be set. This ensures that even if the JWT
// token is missing (for example, on a VM that has rebooted, causing the token to be removed from
// volatile memory), we can still proceed and allow other authentication methods to potentially
// handle the request, such as mTLS.
func (t *DefaultTokenProvider) GetToken() (string, error) {
	if t.opts.CredFetcher == nil {
		return "", nil
	}
	token, err := t.opts.CredFetcher.GetPlatformCredential()
	if err != nil {
		return "", fmt.Errorf("fetch platform credential: %v", err)
	}

	return token, nil
}
