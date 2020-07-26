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

package client_test

import (
	"fmt"
	"testing"

	"istio.io/istio/mixer/test/client/env"
)

// The Istio authn envoy config
const authnConfig = `
- name: istio_authn
  config: {
    "policy": {
      "peers": [
        {
          "mtls": {
            "allow_tls": true
          }
        }
      ],
      "principal_binding": 0
    }
  }
`

const respExpected = "Peer authentication failed."

func TestAuthnPeerRejectNoTls(t *testing.T) {
	s := env.NewTestSetup(env.IstioAuthnTestPeerRejectNoTLS, t)
	// In the Envoy config, requires TLS for peer
	s.SetFiltersBeforeMixer(authnConfig)
	// Disable the HotRestart of Envoy
	s.SetDisableHotRestart(true)

	env.SetStatsUpdateInterval(s.MfConfig(), 1)
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", s.Ports().ClientProxyPort)

	// Issues a GET echo request with 0 size body
	tag := "OKGet"

	// No TLS connection required by Istio authn peer policy.
	// The request will be rejected by Istio authn filter.
	code, resp, err := env.HTTPGet(url)
	if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	// Verify that the http request is rejected
	if code != 401 {
		t.Errorf("Status code 401 is expected, got %d.", code)
	}
	if resp != respExpected {
		t.Errorf("Expected response: %s, got %s.", respExpected, resp)
	}
}
