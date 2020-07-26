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

// Check attributes from a good POST request
const checkAttributesOkPost = `
{
  "context.protocol": "tcp",
  "context.time": "*",
  "context.reporter.uid": "",
  "mesh1.ip": "[1 1 1 1]",
  "source.ip": "[127 0 0 1]",
  "destination.uid": "",
  "destination.namespace": "",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "connection.mtls": false,
  "origin.ip": "[127 0 0 1]",
  "connection.id": "*"
}
`

// Report attributes from a good POST request
const reportAttributesOkPostOpen = `
{
  "context.protocol": "tcp",
  "context.time": "*",
  "context.reporter.uid": "",
  "mesh1.ip": "[1 1 1 1]",
  "source.ip": "[127 0 0 1]",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "destination.ip": "[127 0 0 1]",
  "destination.port": "*",
  "destination.uid": "",
  "destination.namespace": "",
  "connection.mtls": false,
  "origin.ip": "[127 0 0 1]",
  "check.cache_hit": false,
  "quota.cache_hit": false,
  "connection.received.bytes": "*",
  "connection.received.bytes_total": "*",
  "connection.sent.bytes": "*",
  "connection.sent.bytes_total": "*",
  "connection.id": "*",
  "connection.event": "open"
}
`
const reportAttributesOkPostClose = `
{
  "context.protocol": "tcp",
  "context.time": "*",
  "context.reporter.uid": "",
  "mesh1.ip": "[1 1 1 1]",
  "source.ip": "[127 0 0 1]",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "destination.ip": "[127 0 0 1]",
  "destination.port": "*",
  "destination.uid": "",
  "destination.namespace": "",
  "connection.mtls": false,
  "origin.ip": "[127 0 0 1]",
  "check.cache_hit": false,
  "quota.cache_hit": false,
  "connection.received.bytes": "*",
  "connection.received.bytes_total": "*",
  "connection.sent.bytes": "*",
  "connection.sent.bytes_total": "*",
  "connection.duration": "*",
  "connection.id": "*",
  "connection.event": "close"
}
`

// Report attributes from a failed POST request
const reportAttributesFailPost = `
{
  "context.protocol": "tcp",
  "context.time": "*",
  "context.reporter.uid": "",
  "mesh1.ip": "[1 1 1 1]",
  "source.ip": "*",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "connection.mtls": false,
  "origin.ip": "[127 0 0 1]",
  "check.cache_hit": false,
  "quota.cache_hit": false,
  "connection.received.bytes": "*",
  "connection.received.bytes_total": "*",
  "destination.ip": "[127 0 0 1]",
  "destination.port": "*",
  "destination.uid": "",
  "destination.namespace": "",
  "connection.sent.bytes": 0,
  "connection.sent.bytes_total": 0,
  "connection.duration": "*",
  "check.error_code": 16,
  "check.error_message": "UNAUTHENTICATED",
  "connection.id": "*",
  "connection.event": "close"
}
`

// Stats in Envoy proxy.
var expectedStats = map[string]uint64{
	// Policy check stats
	"tcp_mixer_filter.total_check_calls":             2,
	"tcp_mixer_filter.total_check_cache_hits":        0,
	"tcp_mixer_filter.total_check_cache_misses":      2,
	"tcp_mixer_filter.total_check_cache_hit_accepts": 0,
	"tcp_mixer_filter.total_check_cache_hit_denies":  0,
	"tcp_mixer_filter.total_remote_check_calls":      2,
	"tcp_mixer_filter.total_remote_check_accepts":    1,
	"tcp_mixer_filter.total_remote_check_denies":     1,
	// Quota check stats
	"tcp_mixer_filter.total_quota_calls":                 0,
	"tcp_mixer_filter.total_quota_cache_hits":            0,
	"tcp_mixer_filter.total_quota_cache_misses":          0,
	"tcp_mixer_filter.total_quota_cache_hit_accepts":     0,
	"tcp_mixer_filter.total_quota_cache_hit_denies":      0,
	"tcp_mixer_filter.total_remote_quota_calls":          0,
	"tcp_mixer_filter.total_remote_quota_accepts":        0,
	"tcp_mixer_filter.total_remote_quota_denies":         0,
	"tcp_mixer_filter.total_remote_quota_prefetch_calls": 0,
	// Stats for RPCs to mixer policy server
	"tcp_mixer_filter.total_remote_calls":             2,
	"tcp_mixer_filter.total_remote_call_successes":    2,
	"tcp_mixer_filter.total_remote_call_timeouts":     0,
	"tcp_mixer_filter.total_remote_call_send_errors":  0,
	"tcp_mixer_filter.total_remote_call_other_errors": 0,
	// Report stats
	"tcp_mixer_filter.total_remote_report_calls": 2,
	"tcp_mixer_filter.total_report_calls":        3,
}

func TestTCPMixerFilter(t *testing.T) {
	s := env.NewTestSetup(env.TCPMixerFilterTest, t)
	env.SetStatsUpdateInterval(s.MfConfig(), 1)
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", s.Ports().TCPProxyPort)

	// Issues a POST request.
	tag := "OKPost"
	if _, _, err := env.ShortLiveHTTPPost(url, "text/plain", "Hello World!"); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	s.VerifyCheck(tag, checkAttributesOkPost)
	s.VerifyTwoReports(tag, reportAttributesOkPostOpen, reportAttributesOkPostClose)

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
	s.VerifyStats(expectedStats)
}
