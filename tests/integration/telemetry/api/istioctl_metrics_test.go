//go:build integ

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

package api

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/util/retry"
)

// TestIstioctlMetrics contains a basic validation of the experimental
// metrics command. It validates that values are being returned when there is
// traffic and that the expected default output format is matched.
func TestIstioctlMetrics(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			retry.UntilSuccessOrFail(t, func() error {
				if err := SendTraffic(GetClientInstances()[0]); err != nil {
					return err
				}
				return validateDefaultOutput(t, GetTarget().Config().Service, ist.Settings().SystemNamespace)
			}, retry.Delay(framework.TelemetryRetryDelay), retry.Timeout(framework.TelemetryRetryTimeout))
		})
}

func validateDefaultOutput(t framework.TestContext, workload string, systemNamespace string) error { // nolint:interfacer
	t.Helper()
	istioCtl := istioctl.NewOrFail(t, istioctl.Config{})
	args := []string{"experimental", "metrics", workload, "-i", systemNamespace}
	output, stderr, fErr := istioCtl.Invoke(args)
	if fErr != nil {
		t.Logf("Unwanted exception for 'istioctl %s': %v. Stderr: %v", strings.Join(args, " "), fErr, stderr)
		return fErr
	}

	// output will be something like
	//      WORKLOAD    TOTAL RPS    ERROR RPS  P50 LATENCY  P90 LATENCY  P99 LATENCY
	//        server        0.182        0.000         40ms         74ms         97ms
	//
	lines := strings.Split(output, "\n")
	if len(lines) != 3 {
		t.Logf("Expected 2 lines of output, got %q", output)
		return errors.New("unexpected output (incorrect number of lines)")
	}
	fields := strings.Fields(lines[1])
	if len(fields) != 6 {
		t.Logf("Expected 6 columns, got %#v", fields)
		return errors.New("unexpected output (incorrect number of columns)")
	}
	if fields[0] != workload {
		t.Logf("Expected column 1 to be %q, got %#v", workload, fields)
		return errors.New("unexpected output (incorrect workload)")
	}
	totalRPS, fErr := strconv.ParseFloat(fields[1], 32)
	if fErr != nil {
		t.Logf("Expected column 2 to show totalRPS, got %#v", fields)
		return fErr
	}
	if totalRPS <= 0.001 {
		t.Logf("Expected column 2 to show totalRPS more than 0.001, got %#v", fields)
		return errors.New("unexpected output (incorrect RPS)")
	}
	return nil
}
