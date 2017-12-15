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

	"istio.io/istio/security/pkg/registry"
)

type authorizer interface {
	authorize(requester *caller, requestedIds []string) error
}

// sameIDAuthorizer approves a request if the requested identities matches the
// identities of the requester.
// nolint
type sameIDAuthorizer struct{}

func (authZ *sameIDAuthorizer) authorize(requester *caller, requestedIDs []string) error {
	if requester.authSource == authSourceIDToken {
		// TODO: currently the "sub" claim of an ID token returned by GCP
		// metadata server contains obfuscated ID, so we cannot do
		// authorization upon that.
		return nil
	}

	idMap := make(map[string]bool, len(requester.identities))
	for _, id := range requester.identities {
		idMap[id] = true
	}

	for _, requestedID := range requestedIDs {
		if _, exists := idMap[requestedID]; !exists {
			return fmt.Errorf("the requested identity (%q) does not match the caller's identities", requestedID)
		}
	}

	return nil
}

// registryAuthorizor uses an underlying identity registry to make authorization decisions
type registryAuthorizor struct {
	reg registry.Registry
}

func (authZ *registryAuthorizor) authorize(requestor *caller, requestedIDs []string) error {
	// if auth source is JWT token, only check if any of its identities is registered,
	// as we cannot predict what ID may it request yet
	if requestor.authSource == authSourceIDToken {
		valid := false
		for _, identity := range requestor.identities {
			if authZ.reg.Check(identity, identity) {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("the requestor (%v) is not registered", requestor)
		}
		// add the requestedIDs to the registry
		for _, requestedID := range requestedIDs {
			authZ.reg.AddMapping(requestedID, requestedID)
		}
		return nil
	}

	// if auth source is certificate, for each requested ID we require an identity
	// from caller that supports it in registry
	for _, requestedID := range requestedIDs {
		valid := false
		for _, identity := range requestor.identities {
			if authZ.reg.Check(identity, requestedID) {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("the requested identity %q is not authorized", requestedID)
		}
	}
	return nil
}
