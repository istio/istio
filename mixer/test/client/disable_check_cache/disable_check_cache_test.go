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

// Stats in Envoy proxy.
var expectedStats = map[string]uint64{
	// Policy check stats
	"http_mixer_filter.total_check_calls":             10,
	"http_mixer_filter.total_check_cache_hits":        0,
	"http_mixer_filter.total_check_cache_misses":      10,
	"http_mixer_filter.total_check_cache_hit_accepts": 0,
	"http_mixer_filter.total_check_cache_hit_denies":  0,
	"http_mixer_filter.total_remote_check_calls":      10,
	"http_mixer_filter.total_remote_check_accepts":    10,
	"http_mixer_filter.total_remote_check_denies":     0,
	// Quota check stats
	"http_mixer_filter.total_quota_calls":                 0,
	"http_mixer_filter.total_quota_cache_hits":            0,
	"http_mixer_filter.total_quota_cache_misses":          0,
	"http_mixer_filter.total_quota_cache_hit_accepts":     0,
	"http_mixer_filter.total_quota_cache_hit_denies":      0,
	"http_mixer_filter.total_remote_quota_calls":          0,
	"http_mixer_filter.total_remote_quota_accepts":        0,
	"http_mixer_filter.total_remote_quota_denies":         0,
	"http_mixer_filter.total_remote_quota_prefetch_calls": 0,
	// Stats for RPCs to mixer policy server
	"http_mixer_filter.total_remote_calls":             10,
	"http_mixer_filter.total_remote_call_successes":    10,
	"http_mixer_filter.total_remote_call_timeouts":     0,
	"http_mixer_filter.total_remote_call_send_errors":  0,
	"http_mixer_filter.total_remote_call_other_errors": 0,
	// Report stats
	"http_mixer_filter.total_remote_report_calls": 1,
	"http_mixer_filter.total_report_calls":        10,
}

func TestDisableCheckCache(t *testing.T) {
	s := env.NewTestSetup(env.DisableCheckCacheTest, t)
	env.SetStatsUpdateInterval(s.MfConfig(), 1)

	// Disable check cache.
	env.DisableHTTPClientCache(s.MfConfig().HTTPServerConf, true, false, false)

	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", s.Ports().ClientProxyPort)

	// Issues a GET echo request with 0 size body
	tag := "OKGet"
	for i := 0; i < 10; i++ {
		if _, _, err := env.HTTPGet(url); err != nil {
			t.Errorf("Failed in request %s: %v", tag, err)
		}
	}
	// Check is called 10 time.
	s.VerifyCheckCount(tag, 10)

	// Check stats for Check, Quota and report calls.
	s.VerifyStats(expectedStats)
}
