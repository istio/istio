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

package tcpFilter

import (
	"fmt"
	"testing"

	rpc "github.com/gogo/googleapis/google/rpc"

	"istio.io/istio/mixer/test/client/env"
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
  "target.namespace": "XYZ222",
  "connection.mtls": false,
  "connection.id": "*"
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
  "destination.port": "*",
  "connection.mtls": false,
  "connection.received.bytes": 178,
  "connection.received.bytes_total": 178,
  "connection.sent.bytes": 133,
  "connection.sent.bytes_total": 133,
  "connection.duration": "*",
  "connection.id": "*"
}
`

// Report attributes from a failed POST request
const reportAttributesFailPost = `
{
  "context.protocol": "tcp",
  "context.time": "*",
  "mesh1.ip": "[1 1 1 1]",
  "source.ip": "[127 0 0 1]",
  "source.port": "*",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "connection.mtls": false,
  "connection.received.bytes": 178,
  "connection.received.bytes_total": 178,
  "destination.ip": "[127 0 0 1]",
  "destination.port": "*",
  "connection.sent.bytes": 0,
  "connection.sent.bytes_total": 0,
  "connection.duration": "*",
  "check.error_code": 16,
  "check.error_message": "UNAUTHENTICATED",
  "connection.id": "*"
}
`

// Stats in Envoy proxy.
var expectedStats = map[string]int{
	"tcp_mixer_filter.total_blocking_remote_check_calls": 2,
	"tcp_mixer_filter.total_blocking_remote_quota_calls": 0,
	"tcp_mixer_filter.total_check_calls":                 2,
	"tcp_mixer_filter.total_quota_calls":                 0,
	"tcp_mixer_filter.total_remote_check_calls":          2,
	"tcp_mixer_filter.total_remote_quota_calls":          0,
	"tcp_mixer_filter.total_remote_report_calls":         2,
	"tcp_mixer_filter.total_report_calls":                2,
}

func TestTCPMixerFilter(t *testing.T) {
	s := env.NewTestSetup(env.TCPMixerFilterTest, t)
	env.SetStatsUpdateInterval(s.MfConfig(), 1)
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	// Make sure tcp port is ready before starting the test.
	env.WaitForPort(s.Ports().TCPProxyPort)

	url := fmt.Sprintf("http://localhost:%d/echo", s.Ports().TCPProxyPort)

	// Issues a POST request.
	tag := "OKPost"
	if _, _, err := env.ShortLiveHTTPPost(url, "text/plain", "Hello World!"); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	s.VerifyCheck(tag, checkAttributesOkPost)
	s.VerifyReport(tag, reportAttributesOkPost)

	tag = "MixerFail"
	s.SetMixerCheckStatus(rpc.Status{
		Code: int32(rpc.UNAUTHENTICATED),
	})
	if _, _, err := env.ShortLiveHTTPPost(url, "text/plain", "Hello World!"); err == nil {
		t.Errorf("Expect request to fail %s: %v", tag, err)
	}
	// Reset to a positive one
	s.SetMixerCheckStatus(rpc.Status{})
	s.VerifyCheck(tag, checkAttributesOkPost)
	s.VerifyReport(tag, reportAttributesFailPost)

	// Check stats for Check, Quota and report calls.
	if respStats, err := s.WaitForStatsUpdateAndGetStats(2); err == nil {
		s.VerifyStats(respStats, expectedStats)
	} else {
		t.Errorf("Failed to get stats from Envoy %v", err)
	}
}
