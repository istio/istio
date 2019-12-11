// Copyright 2019 Istio Authors
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

package metrics

import (
	"strconv"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istioctl"
)

func testIstioctl(t *testing.T, ctx framework.TestContext, workload string) { // nolint:interfacer
	istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})
	args := []string{"experimental", "metrics", workload}
	output, fErr := istioCtl.Invoke(args)
	if fErr != nil {
		t.Fatalf("Unwanted exception for 'istioctl %s': %v", strings.Join(args, " "), fErr)
	}

	// output will be something like
	//                                   WORKLOAD    TOTAL RPS    ERROR RPS  P50 LATENCY  P90 LATENCY  P99 LATENCY
	//                             productpage-v1        0.182        0.000         40ms         74ms         97ms
	//
	lines := strings.Split(output, "\n")
	if len(lines) != 3 {
		t.Fatalf("Expected 2 lines of output, got %q", output)
	}
	fields := strings.Fields(lines[1])
	if len(fields) != 6 {
		t.Fatalf("Expected 6 columns, got %#v", fields)
	}
	if fields[0] != "productpage-v1" {
		t.Fatalf("Expected column 1 to be 'productpage-v1', got %#v", fields)
	}
	totalRPS, fErr := strconv.ParseFloat(fields[1], 32)
	if fErr != nil {
		t.Fatalf("Expected column 2 to show totalRPS, got %#v", fields)
	}
	if totalRPS <= 0.001 {
		t.Fatalf("Expected column 2 to show totalRPS more than 0.001, got %#v", fields)
	}
}
