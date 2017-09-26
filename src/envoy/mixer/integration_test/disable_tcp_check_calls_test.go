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

// Check attributes from a good POST request
const checkAttributesOkPost = `
{
  "context.protocol": "tcp",
  "context.time": "*",
  "mesh1.ip": "[1 1 1 1]",
  "source.ip": "[127 0 0 1]",
  "source.port": "*",
  "target.uid": "POD222",
  "target.namespace": "XYZ222"
}
`

// Report attributes from a good POST request
const reportAttributesOkPost = `
{
  "context.protocol": "tcp",
  "context.time": "*",
  "mesh1.ip": "[1 1 1 1]",
  "source.ip": "[127 0 0 1]",
  "source.port": "*",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "destination.ip": "[127 0 0 1]",
  "destination.port": 28080,
  "connection.received.bytes": 178,
  "connection.received.bytes_total": 178,
  "connection.sent.bytes": 133,
  "connection.sent.bytes_total": 133,
  "connection.duration": "*",
  "check.status": 0
}
`

func TestDisableTcpCheckCalls(t *testing.T) {
	s := &TestSetup{
		t:    t,
		conf: basicConfig + "," + disableTcpCheckCalls,
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
	s.VerifyCheckCount(tag, 0)
	s.VerifyReport(tag, reportAttributesOkPost)
}
