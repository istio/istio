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

// Report attributes from a good POST request
const openReportAttributesOkPost = `
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
  "connection.received.bytes": 191,
  "connection.received.bytes_total": 191,
  "connection.sent.bytes": 0,
  "connection.sent.bytes_total": 0,
  "connection.id": "*",
  "connection.event": "open"
}
`
const deltaReportAttributesOkPost = `
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
  "connection.received.bytes": 191,
  "connection.received.bytes_total": 191,
  "connection.sent.bytes": 0,
  "connection.sent.bytes_total": 0,
  "connection.id": "*",
  "connection.event": "continue"
}
`
const finalReportAttributesOkPost = `
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
  "connection.received.bytes": 0,
  "connection.received.bytes_total": 191,
  "connection.sent.bytes": "*",
  "connection.sent.bytes_total": "*",
  "connection.duration": "*",
  "connection.id": "*",
  "connection.event": "close"
}
`

// Stats in Envoy proxy.
var expectedStats = map[string]uint64{
	// Policy check stats
	"tcp_mixer_filter.total_check_calls":             1,
	"tcp_mixer_filter.total_check_cache_hits":        0,
	"tcp_mixer_filter.total_check_cache_misses":      1,
	"tcp_mixer_filter.total_check_cache_hit_accepts": 0,
	"tcp_mixer_filter.total_check_cache_hit_denies":  0,
	"tcp_mixer_filter.total_remote_check_calls":      1,
	"tcp_mixer_filter.total_remote_check_accepts":    1,
	"tcp_mixer_filter.total_remote_check_denies":     0,
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
	"tcp_mixer_filter.total_remote_calls":             1,
	"tcp_mixer_filter.total_remote_call_successes":    1,
	"tcp_mixer_filter.total_remote_call_timeouts":     0,
	"tcp_mixer_filter.total_remote_call_send_errors":  0,
	"tcp_mixer_filter.total_remote_call_other_errors": 0,
	// Report stats
	"tcp_mixer_filter.total_remote_report_calls": 3,
	"tcp_mixer_filter.total_report_calls":        3,
}

func TestTCPMixerFilterPeriodicalReport(t *testing.T) {
	s := env.NewTestSetup(env.TCPMixerFilterPeriodicalReportTest, t)
	env.SetTCPReportInterval(s.MfConfig().TCPServerConf, 2)
	env.SetStatsUpdateInterval(s.MfConfig(), 1)
	// Disable check cache.
	env.DisableTCPClientCache(s.MfConfig().TCPServerConf, true, true, true)
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	// Sends a request with parameter delay=3, so that server sleeps 3 seconds and sends response.
	// Mixerclient sends a delta report after 2 seconds, and sends a final report after another 1
	// second.
	url := fmt.Sprintf("http://localhost:%d/echo?delay=3", s.Ports().TCPProxyPort)

	tag := "OKPost"
	if _, _, err := env.ShortLiveHTTPPost(url, "text/plain", "Get Slow Response"); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}

	s.VerifyReport("openReport", openReportAttributesOkPost)
	s.VerifyReport("deltaReport", deltaReportAttributesOkPost)
	s.VerifyReport("finalReport", finalReportAttributesOkPost)

	// Check stats for Check, Quota and report calls.
	s.VerifyStats(expectedStats)
}
