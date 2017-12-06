// Copyright 2017 Istio Authors. All Rights Reserved.
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

package test

import (
	"fmt"
	"testing"
)

// Mixer server not running.
func TestNetworkFailure(t *testing.T) {
	s := &TestSetup{
		t:       t,
		conf:    basicConfig,
		noMixer: true,
	}
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", ServerProxyPort)

	tag := "Fail-Open-V1"
	// Default is fail open policy.
	code, _, err := HTTPGet(url)
	if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	if code != 200 {
		t.Errorf("Status code 200 is expected, got %d.", code)
	}

	s.conf = basicConfig + "," + networkFailClose
	s.ReStartEnvoy()

	tag = "Fail-Close-V1"
	// Use fail close policy.
	code, _, err = HTTPGet(url)
	if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	if code != 503 {
		t.Errorf("Status code 503 is expected, got %d.", code)
	}

	s.v2 = GetDefaultV2Conf()
	s.ReStartEnvoy()

	tag = "Fail-Open"
	// Default is fail open policy.
	code, _, err = HTTPGet(url)
	if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	if code != 200 {
		t.Errorf("Status code 200 is expected, got %d.", code)
	}

	// Set to fail_close
	SetNetworPolicy(s.v2.HttpServerConf, false)
	s.ReStartEnvoy()

	tag = "Fail-Close"
	// Use fail close policy.
	code, _, err = HTTPGet(url)
	if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	if code != 503 {
		t.Errorf("Status code 503 is expected, got %d.", code)
	}
}
