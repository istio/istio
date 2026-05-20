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

package common

import (
	"testing"

	"istio.io/istio/pkg/slices"
)

func TestZtunnelStatsURLs(t *testing.T) {
	got := ZtunnelStatsURLs("")
	if !slices.Contains(got, "stats/prometheus") {
		t.Errorf("expected stats/prometheus in ZtunnelStatsURLs, got %v", got)
	}
}

func TestZtunnelStatsURLs_UnknownVersionFallsBackToLatest(t *testing.T) {
	got := ZtunnelStatsURLs("0.0.0-not-a-real-version")
	if !slices.Contains(got, "stats/prometheus") {
		t.Errorf("expected fallback to latest, got %v", got)
	}
}

func TestZtunnelDebugURLsExcludesStats(t *testing.T) {
	// Stats live on a different port and must not be fetched via the admin port URL list.
	for _, u := range ZtunnelDebugURLs("") {
		if u == "stats/prometheus" || u == "stats" {
			t.Errorf("stats URLs must not appear in ZtunnelDebugURLs (admin-port list): %q", u)
		}
	}
}
