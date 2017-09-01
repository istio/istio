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

import "testing"

func TestAuthroizer(t *testing.T) {
	testCases := map[string]struct {
		authorized   bool
		requestedIDs []string
		userIDs      []string
	}{
		"Authorized": {
			authorized:   true,
			requestedIDs: []string{"id1"},
			userIDs:      []string{"id1", "id2"},
		},
		"Unauthorized": {
			authorized:   false,
			requestedIDs: []string{"id3"},
			userIDs:      []string{"id1", "id2"},
		},
	}

	authz := &simpleAuthorizer{}
	for id, tc := range testCases {
		result := authz.authorize(&user{authSourceClientCertificate, tc.userIDs}, tc.requestedIDs)
		if tc.authorized != result {
			t.Errorf("Case %q: unexpected authorization result: want %t but got %t", id, tc.authorized, result)
		}
	}
}
