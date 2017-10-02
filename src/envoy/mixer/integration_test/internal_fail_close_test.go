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

	rpc "github.com/googleapis/googleapis/google/rpc"
)

func TestFailCloseForMixerInternal(t *testing.T) {
	s := &TestSetup{
		t:    t,
		conf: basicConfig + "," + networkFailClose,
	}
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", ClientProxyPort)

	tag := "Fail-Close"
	// Mixer to return INTERNAL error.
	s.mixer.check.r_status = rpc.Status{
		Code: int32(rpc.INTERNAL),
	}
	code, _, err := HTTPGet(url)
	if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	// Since fail_close policy, expect 500.
	if code != 500 {
		t.Errorf("Status code 500 is expected, got %d.", code)
	}
}
