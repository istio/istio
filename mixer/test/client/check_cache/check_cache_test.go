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

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/test/client/env"
)

// Stats in Envoy proxy.
var expectedStats = map[string]int{
	"http_mixer_filter.total_blocking_remote_check_calls": 1,
	"http_mixer_filter.total_blocking_remote_quota_calls": 0,
	"http_mixer_filter.total_check_calls":                 10,
	"http_mixer_filter.total_quota_calls":                 0,
	"http_mixer_filter.total_remote_check_calls":          1,
	"http_mixer_filter.total_remote_quota_calls":          0,
	"http_mixer_filter.total_remote_report_calls":         1,
	"http_mixer_filter.total_report_calls":                10,
}

func TestCheckCache(t *testing.T) {
	s := env.NewTestSetup(env.CheckCacheTest, t)
	env.SetStatsUpdateInterval(s.MfConfig(), 1)
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

	// Issues a GET echo request with 0 size body
	tag := "OKGet"
	for i := 0; i < 10; i++ {
		if _, _, err := env.HTTPGet(url); err != nil {
			t.Errorf("Failed in request %s: %v", tag, err)
		}
	}
	// Only the first check is called.
	s.VerifyCheckCount(tag, 1)

	// Check stats for Check, Quota and report calls.
	s.VerifyStats(expectedStats)
}
