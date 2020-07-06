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

package xds

import (
	"context"
	"errors"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"

	"istio.io/istio/security/pkg/server/ca/authenticate"
)

// authenticate authenticates the ADS request using the configured authenticators.
// Returns the validated principals or an error.
// If no authenticators are configured, or if the request is on a non-secure
// stream ( 15010 ) - returns an empty list of principals and no errors.
func (s *DiscoveryServer) authenticate(ctx context.Context) ([]string, error) {
	// Authenticate - currently just checks that request has a certificate signed with the our key.
	// Protected by flag to avoid breaking upgrades - should be enabled in multi-cluster/meshexpansion where
	// XDS is exposed.
	if len(s.Authenticators) > 0 {
		peerInfo, ok := peer.FromContext(ctx)
		if !ok {
			return nil, errors.New("invalid context")
		}
		// Not a TLS connection, we will not authentication
		// TODO: add a flag to prevent unauthenticated requests ( 15010 )
		// request not over TLS ( on the insecure port
		if _, ok := peerInfo.AuthInfo.(credentials.TLSInfo); !ok {
			return nil, nil
		}
		var authenticatedID *authenticate.Caller
		for _, authn := range s.Authenticators {
			u, err := authn.Authenticate(ctx)
			if u != nil && err == nil {
				authenticatedID = u
				break
			}
		}

		if authenticatedID == nil {
			adsLog.Errora("Failed to authenticate client ", peerInfo)
			return nil, errors.New("authentication failure")
		}

		return authenticatedID.Identities, nil
	}
	return nil, nil
}
