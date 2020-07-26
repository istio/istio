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

// Mixer server not running.
func TestNetworkFailure(t *testing.T) {
	s := env.NewTestSetup(env.NetworkFailureTest, t)
	s.SetNoMixer(true)
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", s.Ports().ServerProxyPort)

	tag := "Fail-Open"
	// Default is fail open policy.
	code, _, err := env.HTTPGet(url)
	if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	if code != 200 {
		t.Errorf("Status code 200 is expected, got %d.", code)
	}

	// Set to fail_close
	env.SetNetworPolicy(s.MfConfig().HTTPServerConf, false)
	s.ReStartEnvoy()

	tag = "Fail-Close"
	url = fmt.Sprintf("http://localhost:%d/echo", s.Ports().ServerProxyPort)
	// Use fail close policy.
	code, _, err = env.HTTPGet(url)
	if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	if code == 200 {
		t.Errorf("Non-200 status code is expected, got %d.", code)
	}
}
