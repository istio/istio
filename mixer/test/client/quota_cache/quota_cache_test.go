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

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/test/client/env"
)

// Stats in Envoy proxy.
var expectedStats = map[string]uint64{
	// Policy check stats
	"http_mixer_filter.total_check_calls":             20,
	"http_mixer_filter.total_check_cache_hits":        19,
	"http_mixer_filter.total_check_cache_misses":      1,
	"http_mixer_filter.total_check_cache_hit_accepts": 19,
	"http_mixer_filter.total_check_cache_hit_denies":  0,
	"http_mixer_filter.total_remote_check_calls":      1,
	"http_mixer_filter.total_remote_check_accepts":    1,
	"http_mixer_filter.total_remote_check_denies":     0,
	// Quota check stats
	"http_mixer_filter.total_quota_calls":             20,
	"http_mixer_filter.total_quota_cache_hits":        19,
	"http_mixer_filter.total_quota_cache_misses":      1,
	"http_mixer_filter.total_quota_cache_hit_accepts": 9,
	"http_mixer_filter.total_quota_cache_hit_denies":  10,
	// Report stats
	"http_mixer_filter.total_remote_report_calls": 1,
	"http_mixer_filter.total_report_calls":        20,
}

func TestQuotaCache(t *testing.T) {
	t.Skip("https://github.com/istio/istio/issues/20092")
	// Only check cache is enabled, quota cache is enabled.
	s := env.NewTestSetup(env.QuotaCacheTest, t)
	env.SetStatsUpdateInterval(s.MfConfig(), 1)
	env.AddHTTPQuota(s.MfConfig(), "RequestCount", 1)
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", s.Ports().ClientProxyPort)

	// Need to override mixer test server Referenced field in the check response.
	// Its default is all fields in the request which could not be used fo test check cache.
	output := mixerpb.ReferencedAttributes{
		AttributeMatches: make([]mixerpb.ReferencedAttributes_AttributeMatch, 1),
	}
	output.AttributeMatches[0] = mixerpb.ReferencedAttributes_AttributeMatch{
		// Assume "target.name" is in the request attributes, and it is used for Check.
		Name:      10,
		Condition: mixerpb.EXACT,
	}
	s.SetMixerCheckReferenced(&output)
	s.SetMixerQuotaReferenced(&output)

	// Issues a GET echo request with 0 size body
	tag := "OKGet"
	s.SetMixerQuotaLimit(10)
	reject := 0
	ok := 0
	for i := 0; i < 20; i++ {
		code, _, err := env.HTTPGet(url)
		if err != nil {
			t.Errorf("Failed in request %s: %v", tag, err)
		}
		if code == 200 {
			ok++
		} else if code == 429 {
			reject++
		}
	}
	if ok+reject < 20 {
		t.Fatalf("sum of ok count %v and reject count %v is less than 20", ok, reject)
	}
	// ok should be around 10, allow 30% margin (prefetch code may have some margin).
	if ok > 13 || ok < 7 {
		t.Fatalf("Unexpected quota ok count %v, reject count %v", ok, reject)
	}
	// Less than 5 time of Quota is called.
	if s.GetMixerQuotaCount() >= 6 {
		t.Fatalf("%s quota called count %v should not be more than 5",
			tag, s.GetMixerQuotaCount())
	}

	// Check stats for Check, Quota and report calls.
	s.VerifyStats(expectedStats)
	if respStats, err := s.WaitForStatsUpdateAndGetStats(2); err == nil {
		// Because prefetch code may have some margin, actual number of check and quota calls are not
		// determined.
		s.VerifyStatsLT(respStats, "http_mixer_filter.total_remote_calls", 5)
		s.VerifyStatsLT(respStats, "http_mixer_filter.total_remote_check_calls", 5)
		s.VerifyStatsLT(respStats, "http_mixer_filter.total_remote_quota_calls", 5)
		s.VerifyStatsLT(respStats, "http_mixer_filter.total_remote_quota_prefetch_calls", 5)
		s.VerifyStatsLT(respStats, "http_mixer_filter.total_remote_report_calls", 5)
	} else {
		t.Errorf("Failed to get stats from Envoy %v", err)
	}
}
