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

import "github.com/golang/glog"

type authorizer interface {
	authorize(requester *user, requestedIds []string) bool
}

// simpleAuthorizer approves a request if the requested identities matches the
// identities of the requester.
type simpleAuthorizer struct{}

func (authZ *simpleAuthorizer) authorize(requester *user, requestedIDs []string) bool {
	if requester.authSource == authSourceIDToken {
		// TODO: currently the "sub" claim of an ID token returned by GCP
		// metadata server contains obfuscated user ID, so we cannot do
		// authorization upon that.

		return true
	}

	idMap := make(map[string]bool, len(requester.identities))
	for _, id := range requester.identities {
		idMap[id] = true
	}

	for _, requestedID := range requestedIDs {
		if _, exists := idMap[requestedID]; !exists {
			glog.Warningf("The requested identity (%q) does not match the requester", requestedID)

			return false
		}
	}

	return true
}
