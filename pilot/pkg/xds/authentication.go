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
	"fmt"
	"strings"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

// authenticate authenticates the ADS request using the configured authenticators.
// Returns the validated principals or an error.
// If no authenticators are configured, or if the request is on a non-secure
// stream ( 15010 ) - returns an empty list of principals and no errors.
func (s *DiscoveryServer) authenticate(ctx context.Context) ([]string, error) {
	if len(s.Authenticators) == 0 {
		return nil, nil
	}

	// Authenticate - currently just checks that request has a certificate signed with the our key.
	// Protected by flag to avoid breaking upgrades - should be enabled in multi-cluster/meshexpansion where
	// XDS is exposed.
	peerInfo, ok := peer.FromContext(ctx)
	if !ok {
		return nil, errors.New("invalid context")
	}
	// Not a TLS connection, we will not perform authentication
	// TODO: add a flag to prevent unauthenticated requests ( 15010 )
	// request not over TLS on the insecure port
	if _, ok := peerInfo.AuthInfo.(credentials.TLSInfo); !ok {
		return nil, nil
	}
	authFailMsgs := []string{}
	for _, authn := range s.Authenticators {
		u, err := authn.Authenticate(ctx)
		// If one authenticator passes, return
		if u != nil && err == nil {
			return u.Identities, nil
		}
		authFailMsgs = append(authFailMsgs, fmt.Sprintf("Authenticator %s: %v", authn.AuthenticatorType(), err))
	}

	adsLog.Errora("Failed to authenticate client from ", peerInfo.Addr.String(), " ", strings.Join(authFailMsgs, ";"))
	return nil, errors.New("authentication failure")
}
