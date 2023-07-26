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
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/spiffe"
)

// authenticate authenticates the ADS request using the configured authenticators.
// Returns the validated principals or an error.
// If no authenticators are configured, or if the request is on a non-secure
// stream ( 15010 ) - returns an empty list of principals and no errors.
func (s *DiscoveryServer) authenticate(ctx context.Context) ([]string, error) {
	c, err := security.Authenticate(ctx, s.Authenticators)
	if c != nil {
		return c.Identities, nil
	}
	return nil, err
}

func (s *DiscoveryServer) authorize(con *Connection, identities []string) error {
	if con == nil || con.proxy == nil {
		return nil
	}

	if features.EnableXDSIdentityCheck && identities != nil {
		// TODO: allow locking down, rejecting unauthenticated requests.
		id, err := checkConnectionIdentity(con.proxy, identities)
		if err != nil {
			log.Warnf("Unauthorized XDS: %v with identity %v: %v", con.peerAddr, identities, err)
			return status.Newf(codes.PermissionDenied, "authorization failed: %v", err).Err()
		}
		con.proxy.VerifiedIdentity = id
	}
	return nil
}

func checkConnectionIdentity(proxy *model.Proxy, identities []string) (*spiffe.Identity, error) {
	for _, rawID := range identities {
		spiffeID, err := spiffe.ParseIdentity(rawID)
		if err != nil {
			continue
		}
		if proxy.ConfigNamespace != "" && spiffeID.Namespace != proxy.ConfigNamespace {
			continue
		}
		if proxy.Metadata.ServiceAccount != "" && spiffeID.ServiceAccount != proxy.Metadata.ServiceAccount {
			continue
		}
		return &spiffeID, nil
	}
	return nil, fmt.Errorf("no identities (%v) matched %v/%v", identities, proxy.ConfigNamespace, proxy.Metadata.ServiceAccount)
}
