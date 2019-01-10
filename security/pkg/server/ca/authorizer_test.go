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

package ca

import (
	"testing"

	"istio.io/istio/security/pkg/registry"
	"istio.io/istio/security/pkg/server/ca/authenticate"
)

func TestSameIDAuthroizer(t *testing.T) {
	testCases := map[string]struct {
		expectedErr  string
		requestedIDs []string
		callerIDs    []string
	}{
		"Authorized": {
			expectedErr:  "",
			requestedIDs: []string{"id1"},
			callerIDs:    []string{"id1", "id2"},
		},
		"Unauthorized": {
			expectedErr:  "the requested identity (\"id3\") does not match the caller's identities",
			requestedIDs: []string{"id3"},
			callerIDs:    []string{"id1", "id2"},
		},
	}

	authz := &sameIDAuthorizer{}
	for id, tc := range testCases {
		err := authz.authorize(&authenticate.Caller{authenticate.AuthSourceClientCertificate, tc.callerIDs}, tc.requestedIDs)
		if len(tc.expectedErr) > 0 {
			if err == nil {
				t.Errorf("%s: succeeded. Error expected: %v", id, err)
			} else if err.Error() != tc.expectedErr {
				t.Errorf("%s: incorrect error message: %s VS %s", id, err.Error(), tc.expectedErr)
			}
		} else if err != nil {
			t.Errorf("%s: unexpected Error: %v", id, err)
		}
	}
}

func TestRegistryAuthorizerWithJWT(t *testing.T) {
	idRequestor := &authenticate.Caller{
		AuthSource: authenticate.AuthSourceIDToken,
		Identities: []string{"id"},
	}
	requestedIDs := []string{"spiffe://id", "spiffe://id2"}

	testCases := map[string]struct {
		requestor    *authenticate.Caller
		requestedIDs []string
		authorizor   *registryAuthorizor
		expectedErr  string
	}{
		"Unauthorized with empty mapping": {
			requestor:    idRequestor,
			requestedIDs: requestedIDs,
			authorizor:   &registryAuthorizor{&registry.IdentityRegistry{Map: make(map[string]string)}},
			expectedErr:  "the requestor (&{1 [id]}) is not registered",
		},
		"Authorized with one mapping": {
			requestor:    idRequestor,
			requestedIDs: requestedIDs,
			authorizor: &registryAuthorizor{&registry.IdentityRegistry{
				Map: map[string]string{"id": "id"},
			}},
		},
	}

	for id, c := range testCases {
		err := c.authorizor.authorize(c.requestor, c.requestedIDs)
		if c.expectedErr != "" {
			if err == nil {
				t.Errorf("%s: succeeded. Error expected: %v", id, err)
			} else if err.Error() != c.expectedErr {
				t.Errorf("%s: incorrect error message: expected %s, actual %s", id, c.expectedErr, err.Error())
			}
		} else if err != nil {
			t.Errorf("%s: unexpected error: %v", id, err)
		}
	}
}

func TestRegistryAuthorizerWithClientCertificate(t *testing.T) {
	testCases := map[string]struct {
		callerIDs    []string
		requestedIDs []string
		registry     registry.IdentityRegistry
		expectedErr  string
	}{
		"Authorized with one mapping": {
			callerIDs:    []string{"id1"},
			requestedIDs: []string{"id1"},
			registry: registry.IdentityRegistry{
				Map: map[string]string{"id1": "id1"},
			},
		},
		"Unauthorized with empty mapping": {
			callerIDs:    []string{"id1"},
			requestedIDs: []string{"id1"},
			registry: registry.IdentityRegistry{
				Map: map[string]string{},
			},
			expectedErr: "the requested identity \"id1\" is not authorized",
		},
		"Unauthorized with one mapping": {
			callerIDs:    []string{"id1"},
			requestedIDs: []string{"id1"},
			registry: registry.IdentityRegistry{
				Map: map[string]string{"id2": "id2"},
			},
			expectedErr: "the requested identity \"id1\" is not authorized",
		},
		"Authorized with two mappings": {
			callerIDs:    []string{"id1", "id2"},
			requestedIDs: []string{"id3", "id4"},
			registry: registry.IdentityRegistry{
				Map: map[string]string{
					"id1": "id3",
					"id2": "id4",
				},
			},
		},
		"Unauthorized with three mappings": {
			callerIDs:    []string{"id1", "id2", "id3"},
			requestedIDs: []string{"id4", "id5", "id6"},
			registry: registry.IdentityRegistry{
				Map: map[string]string{
					"id1": "id4",
					"id2": "id5",
					"id3": "id5",
				},
			},
			expectedErr: "the requested identity \"id6\" is not authorized",
		},
	}

	for id, c := range testCases { // nolint: vet
		authz := &registryAuthorizor{&c.registry}
		err := authz.authorize(&authenticate.Caller{authenticate.AuthSourceClientCertificate, c.callerIDs}, c.requestedIDs)
		if c.expectedErr != "" {
			if err == nil {
				t.Errorf("%s: succeeded. Error expected: %v", id, err)
			} else if err.Error() != c.expectedErr {
				t.Errorf("%s: incorrect error message: expected %s, actual %s", id, c.expectedErr, err.Error())
			}
		} else if err != nil {
			t.Errorf("%s: unexpected error: %v", id, err)
		}
	}
}
