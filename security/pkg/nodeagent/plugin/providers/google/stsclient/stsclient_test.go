// Copyright 2018 Istio Authors
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

	"istio.io/istio/security/pkg/nodeagent/plugin/providers/google/stsclient/test"
)

func TestGetFederatedToken(t *testing.T) {
	tlsFlag = false
	defer func() {
		tlsFlag = true
	}()

	r := NewPlugin()

	ms, err := test.StartNewServer()
	secureTokenEndpoint = ms.URL + "/v1/identitybindingtoken"
	defer func() {
		ms.Stop()
		secureTokenEndpoint = "https://securetoken.googleapis.com/v1/identitybindingtoken"
	}()

	if err != nil {
		t.Fatalf("failed to start a mock server %v", err)
	}

	token, _, err := r.ExchangeToken(context.Background(), "", "")
	if err != nil {
		t.Fatalf("failed to call exchange token %v", err)
	}
	if got, want := token, "footoken"; got != want {
		t.Errorf("Access token got %q, expected %q", "footoken", token)
	}
}
