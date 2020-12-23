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

package stsclient

import (
	"context"
	"testing"

	"istio.io/istio/security/pkg/stsservice/tokenmanager/google/mock"
)

func TestGetFederatedToken(t *testing.T) {
	GKEClusterURL = mock.FakeGKEClusterURL
	r := NewPlugin()

	ms, err := mock.StartNewServer(t, mock.Config{Port: 0})
	if err != nil {
		t.Fatalf("failed to start a mock server: %v", err)
	}
	SecureTokenEndpoint = ms.URL + "/v1/identitybindingtoken"
	defer func() {
		if err := ms.Stop(); err != nil {
			t.Logf("failed to stop mock server: %v", err)
		}
		SecureTokenEndpoint = "https://securetoken.googleapis.com/v1/identitybindingtoken"
	}()

	token, _, _, err := r.ExchangeToken(context.Background(), nil, mock.FakeTrustDomain, mock.FakeSubjectToken)
	if err != nil {
		t.Fatalf("failed to call exchange token %v", err)
	}
	if token != mock.FakeFederatedToken {
		t.Errorf("Access token got %q, expected %q", token, mock.FakeFederatedToken)
	}
}
