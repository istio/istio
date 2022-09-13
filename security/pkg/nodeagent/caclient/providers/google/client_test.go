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
	"fmt"
	"reflect"
	"testing"

	"istio.io/istio/security/pkg/nodeagent/caclient/providers/google/mock"
)

const mockServerAddress = "localhost:0"

var fakeCert = []string{"foo", "bar"}

func TestGoogleCAClient(t *testing.T) {
	t.Setenv("GKE_CLUSTER_URL", "https://container.googleapis.com/v1/projects/testproj/locations/us-central1-c/clusters/cluster1")

	testCases := map[string]struct {
		service      mock.CAService
		expectedCert []string
		expectedErr  string
	}{
		"Valid certs": {
			service:      mock.CAService{Certs: fakeCert, Err: nil},
			expectedCert: fakeCert,
			expectedErr:  "",
		},
		"Error in response": {
			service:      mock.CAService{Certs: nil, Err: fmt.Errorf("test failure")},
			expectedCert: nil,
			expectedErr:  "rpc error: code = Unknown desc = test failure",
		},
		"Empty response": {
			service:      mock.CAService{Certs: []string{}, Err: nil},
			expectedCert: nil,
			expectedErr:  "invalid response cert chain",
		},
	}

	for id, tc := range testCases {
		// create a local grpc server
		s, err := mock.CreateServer(mockServerAddress, &tc.service)
		if err != nil {
			t.Fatalf("Test case [%s]: failed to create server: %v", id, err)
		}
		defer s.Stop()

		cli, err := NewGoogleCAClient(s.Address, false, nil)
		if err != nil {
			t.Errorf("Test case [%s]: failed to create ca client: %v", id, err)
		}

		resp, err := cli.CSRSign([]byte{0o1}, 1)
		if err != nil {
			if err.Error() != tc.expectedErr {
				t.Errorf("Test case [%s]: error (%s) does not match expected error (%s)", id, err.Error(), tc.expectedErr)
			}
		} else {
			if tc.expectedErr != "" {
				t.Errorf("Test case [%s]: expect error: %s but got no error", id, tc.expectedErr)
			} else if !reflect.DeepEqual(resp, tc.expectedCert) {
				t.Errorf("Test case [%s]: resp: got %+v, expected %v", id, resp, tc.expectedCert)
			}
		}
	}
}

func TestParseZone(t *testing.T) {
	testCases := map[string]struct {
		clusterURL   string
		expectedZone string
	}{
		"Valid URL": {
			clusterURL:   "https://container.googleapis.com/v1/projects/testproj1/locations/us-central1-c/clusters/c1",
			expectedZone: "us-central1-c",
		},
		"Hub URL": {
			clusterURL:   "https://gkehub.googleapis.com/projects/testproject1/locations/global/memberships/test01",
			expectedZone: "",
		},
		"InValid response": {
			clusterURL:   "aaa",
			expectedZone: "",
		},
	}

	for id, tc := range testCases {
		zone := parseZone(tc.clusterURL)
		if zone != tc.expectedZone {
			t.Errorf("Test case [%s]: proj: got %+v, expected %v", id, zone, tc.expectedZone)
		}
	}
}
