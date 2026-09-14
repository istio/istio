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

package xds

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"istio.io/istio/pkg/monitoring"
)

func TestProxyConvergenceTimeBuckets(t *testing.T) {
	const setting = "PILOT_PROXY_CONVERGENCE_TIME_BUCKETS_SECONDS"
	const childCase = "ISTIO_TEST_PROXY_CONVERGENCE_BUCKETS_CASE"
	defaults := []float64{.1, .5, 1, 3, 5, 10, 20, 30}
	cases := []struct {
		name  string
		value string
		want  []float64
	}{
		{"unset", "", defaults},
		{"empty", "", defaults},
		{"custom", "0.01,0.25,1,60,120", []float64{.01, .25, 1, 60, 120}},
		{"invalid", "1,0.1", defaults},
	}
	if name := os.Getenv(childCase); name != "" {
		for _, tt := range cases {
			if tt.name != name {
				continue
			}
			for _, metric := range monitoring.ExportMetricDefinitions() {
				if metric.Name == "pilot_proxy_convergence_time" {
					if !slices.Equal(metric.Bounds, tt.want) {
						t.Fatalf("registered boundaries = %v, want %v", metric.Bounds, tt.want)
					}
					return
				}
			}
			t.Fatal("pilot_proxy_convergence_time was not registered")
		}
		t.Fatalf("unknown child case %q", name)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Both the feature and the metric initialize before tests run, so each
			// configuration must be checked in a fresh process.
			cmd := exec.Command(executable, "-test.run=^TestProxyConvergenceTimeBuckets$")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, setting+"=") && !strings.HasPrefix(entry, childCase+"=") {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			cmd.Env = append(cmd.Env, childCase+"="+tt.name)
			if tt.name != "unset" {
				cmd.Env = append(cmd.Env, setting+"="+tt.value)
			}
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("startup check failed: %v\n%s", err, output)
			}
		})
	}
}
