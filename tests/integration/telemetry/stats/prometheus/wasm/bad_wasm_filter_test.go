//go:build integ
// +build integ

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

package wasm

import (
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/util/retry"
	util "istio.io/istio/tests/integration/telemetry"
	common "istio.io/istio/tests/integration/telemetry/stats/prometheus"
)

// TestBadWasmRemoteLoad tests that bad Wasm remote load configuration won't affect service.
// The test will set up an echo client and server, test echo ping works fine. Then apply a
// Wasm filter which has a bad URL link, which will result as module download failure. After that,
// verifies that echo ping could still work. The test also verifies that metrics are properly
// recorded for module downloading failure and nack on ECDS update.
func TestBadWasmRemoteLoad(t *testing.T) {
	framework.NewTest(t).
		Features("extensibility.wasm.remote-load").
		Run(func(t framework.TestContext) {
			badWasmTestHelper(t, "testdata/bad-filter.yaml", false, true)
		})
}

// TestBadWasmWithFailOpen is basically the same with TestBadWasmRemoteLoad except
// it tests with "fail_open = true". To test the fail_open, the target pod is restarted
// after applying the Wasm filter.
// At this moment, there is no "fail_open" option in WasmPlugin API. So, we test it using
// EnvoyFilter. When WasmPlugin has a "fail_open" option in the API plane, we need to change
// this test to use the WasmPlugin API
func TestBadWasmWithFailOpen(t *testing.T) {
	framework.NewTest(t).
		Features("extensibility.wasm.remote-load").
		Run(func(t framework.TestContext) {
			// since this case is for "fail_open=true", ecds is not rejected.
			badWasmTestHelper(t, "testdata/bad-wasm-envoy-filter-fail-open.yaml", true, false)
		})
}

func badWasmTestHelper(t framework.TestContext, filterConfigPath string, restartTarget bool, ecdsShouldReject bool) {
	t.Helper()
	// Test bad wasm remote load in only one cluster.
	// There is no need to repeat the same testing logic in multiple clusters.
	to := match.Cluster(t.Clusters().Default()).FirstOrFail(t, common.GetClientInstances())
	// Verify that echo server could return 200
	retry.UntilSuccessOrFail(t, func() error {
		if err := common.SendTraffic(to); err != nil {
			return err
		}
		return nil
	}, retry.Delay(1*time.Millisecond), retry.Timeout(5*time.Second))
	t.Log("echo server returns OK, apply bad wasm remote load filter.")

	// Apply bad filter config
	t.Logf("use config in %s.", filterConfigPath)
	t.ConfigIstio().File(common.GetAppNamespace().Name(), filterConfigPath).ApplyOrFail(t)
	if restartTarget {
		target := match.Cluster(t.Clusters().Default()).FirstOrFail(t, common.GetTarget().Instances())
		if err := target.Restart(); err != nil {
			t.Fatalf("failed to restart the target pod: %v", err)
		}
	}

	// Wait until there is agent metrics for wasm download failure
	retry.UntilSuccessOrFail(t, func() error {
		q := prometheus.Query{Metric: "istio_agent_wasm_remote_fetch_count", Labels: map[string]string{"result": "download_failure"}}
		c := to.Config().Cluster
		if _, err := common.QueryPrometheus(t, c, q, common.GetPromInstance()); err != nil {
			util.PromDiff(t, common.GetPromInstance(), c, q)
			return err
		}
		return nil
	}, retry.Delay(1*time.Second), retry.Timeout(80*time.Second))

	if ecdsShouldReject && t.Clusters().Default().IsPrimary() { // Only check istiod if running locally (i.e., not an external control plane)
		// Verify that istiod has a stats about rejected ECDS update
		// pilot_total_xds_rejects{type="type.googleapis.com/envoy.config.core.v3.TypedExtensionConfig"}
		retry.UntilSuccessOrFail(t, func() error {
			q := prometheus.Query{Metric: "pilot_total_xds_rejects", Labels: map[string]string{"type": "ecds"}}
			c := to.Config().Cluster
			if _, err := common.QueryPrometheus(t, c, q, common.GetPromInstance()); err != nil {
				util.PromDiff(t, common.GetPromInstance(), c, q)
				return err
			}
			return nil
		}, retry.Delay(1*time.Second), retry.Timeout(80*time.Second))
	}

	t.Log("got istio_agent_wasm_remote_fetch_count metric in prometheus, bad wasm filter is applied, send request to echo server again.")

	// Verify that echo server could still return 200
	retry.UntilSuccessOrFail(t, func() error {
		if err := common.SendTraffic(to); err != nil {
			return err
		}
		return nil
	}, retry.Delay(1*time.Millisecond), retry.Timeout(10*time.Second))

	t.Log("echo server still returns ok after bad wasm filter is applied.")
}
