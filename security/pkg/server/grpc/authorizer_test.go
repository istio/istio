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
  "testing"
)

func TestAuthroizer(t *testing.T) {
	testCases := map[string]struct {
		expectedErr   string
		requestedIDs []string
		callerIDs      []string
	}{
		"Authorized": {
			expectedErr:   "",
			requestedIDs: []string{"id1"},
			callerIDs:      []string{"id1", "id2"},
		},
		"Unauthorized": {
			expectedErr:   "The requested identity (\"id3\") does not match the caller's identities",
			requestedIDs: []string{"id3"},
			callerIDs:      []string{"id1", "id2"},
		},
	}

	authz := &sameIdAuthorizer{}
	for id, tc := range testCases {
		err := authz.authorize(&caller{authSourceClientCertificate, tc.callerIDs}, tc.requestedIDs)
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
