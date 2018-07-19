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

package client_test

import (
	"fmt"
	"testing"

	"istio.io/istio/mixer/test/client/env"
)

// Report attributes from a good POST request
const reportAttributesOkPost = `
{
  "context.protocol": "tcp",
  "context.time": "*",
  "mesh1.ip": "[1 1 1 1]",
  "source.ip": "[127 0 0 1]",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "destination.ip": "[127 0 0 1]",
  "destination.port": "*",
  "connection.mtls": false,
  "connection.received.bytes": "*",
  "connection.received.bytes_total": "*",
  "connection.sent.bytes": "*",
  "connection.sent.bytes_total": "*",
  "connection.duration": "*",
  "connection.id": "*",
  "connection.event": "close"
}
`

// Stats in Envoy proxy.
var expectedStats = map[string]int{
	"tcp_mixer_filter.total_blocking_remote_check_calls": 0,
	"tcp_mixer_filter.total_blocking_remote_quota_calls": 0,
	"tcp_mixer_filter.total_check_calls":                 0,
	"tcp_mixer_filter.total_quota_calls":                 0,
	"tcp_mixer_filter.total_remote_check_calls":          0,
	"tcp_mixer_filter.total_remote_quota_calls":          0,
	"tcp_mixer_filter.total_remote_report_calls":         1,
	"tcp_mixer_filter.total_report_calls":                1,
}

func TestDisableTCPCheckCalls(t *testing.T) {
	s := env.NewTestSetup(env.DisableTCPCheckCallsTest, t)
	env.SetStatsUpdateInterval(s.MfConfig(), 1)
	// Disable Check
	env.DisableTCPCheckReport(s.MfConfig().TCPServerConf, true, false)

	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", s.Ports().TCPProxyPort)

	// Issues a POST request.
	tag := "OKPost v1"
	if _, _, err := env.ShortLiveHTTPPost(url, "text/plain", "Hello World!"); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	s.VerifyCheckCount(tag, 0)
	s.VerifyReport(tag, reportAttributesOkPost)
	s.VerifyStats(expectedStats)
}
