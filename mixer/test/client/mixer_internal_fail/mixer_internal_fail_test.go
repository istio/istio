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

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	"istio.io/istio/mixer/test/client/env"
)

// Mixer server returns INTERNAL failure.
func TestMixerInternalFail(t *testing.T) {
	s := env.NewTestSetup(env.MixerInternalFailTest, t)
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", s.Ports().ServerProxyPort)

	// Mixer to return INTERNAL error.
	s.SetMixerCheckStatus(rpc.Status{
		Code: int32(rpc.INTERNAL),
	})

	tag := "Fail-Open"
	code, _, err := env.HTTPGet(url)
	if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	// Since fail_open policy by default, expect 200.
	if code != 200 {
		t.Errorf("Status code 200 is expected, got %d.", code)
	}

	// Set to fail_close
	env.SetNetworPolicy(s.MfConfig().HTTPServerConf, false)
	s.ReStartEnvoy()

	tag = "Fail-Close"
	// Use fail close policy.
	url = fmt.Sprintf("http://localhost:%d/echo", s.Ports().ServerProxyPort)
	code, _, err = env.HTTPGet(url)
	if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	// Since fail_close policy, expect 500.
	if code != 500 {
		t.Errorf("Status code 500 is expected, got %d.", code)
	}
}
