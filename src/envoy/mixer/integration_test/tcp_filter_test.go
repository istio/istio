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

// Check attributes from a good POST request
const checkAttributesOkPost = `
{
  "source.ip": "127.0.0.1",
  "source.port": "*",
  "local.ip": "127.0.0.1",
  "local.port": 26060,
  "request.time": "*",
  "target.uid": "POD222",
  "target.namespace": "XYZ222"
}
`

// Report attributes from a good POST request
const reportAttributesOkPost = `
{
  "source.ip": "127.0.0.1",
  "source.port": "*",
  "local.ip": "127.0.0.1",
  "local.port": 26060,
  "request.time": "*",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "target.ip": "127.0.0.1",
  "target.port": 28080,
  "request.size": 178,
  "response.size": 133,
  "check.status": 0,
  "response.time": "*"
}
`

// Report attributes from a failed POST request
const reportAttributesFailPost = `
{
  "source.ip": "127.0.0.1",
  "source.port": "*",
  "local.ip": "127.0.0.1",
  "local.port": 26060,
  "request.time": "*",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "request.size": 0,
  "response.size": 0,
  "check.status": 16,
  "response.time": "*"
}
`

func TestTcpMixerFilter(t *testing.T) {
	s := &TestSetup{
		t:    t,
		conf: basicConfig,
	}
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", TcpProxyPort)

	// Issues a POST request.
	tag := "OKPost"
	if _, _, err := ShortLiveHTTPPost(url, "text/plain", "Hello World!"); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	s.VerifyCheck(tag, checkAttributesOkPost)
	s.VerifyReport(tag, reportAttributesOkPost)

	tag = "MixerFail"
	s.mixer.check.r_status = rpc.Status{
		Code: int32(rpc.UNAUTHENTICATED),
	}
	if _, _, err := ShortLiveHTTPPost(url, "text/plain", "Hello World!"); err == nil {
		t.Errorf("Expect request to fail %s: %v", tag)
	}
	s.VerifyCheck(tag, checkAttributesOkPost)
	s.VerifyReport(tag, reportAttributesFailPost)
}
