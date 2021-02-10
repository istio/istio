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
	"fmt"
	"io/ioutil"
	"testing"
	"time"

	resource "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
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
		Run(func(ctx framework.TestContext) {
			// Test bad wasm remote load in only one cluster.
			// There is no need to repeat the same testing logic in several different clusters.
			cltInstance := common.GetClientInstances().GetOrFail(ctx, echo.InCluster(ctx.Clusters().Default()))
			// Verify that echo server could return 200
			retry.UntilSuccessOrFail(t, func() error {
				if err := common.SendTraffic(cltInstance); err != nil {
					return err
				}
				return nil
			}, retry.Delay(1*time.Millisecond), retry.Timeout(5*time.Second))
			t.Log("echo server returns OK, apply bad wasm remote load filter.")

			// Apply bad filter config
			content, err := ioutil.ReadFile("testdata/bad-filter.yaml")
			if err != nil {
				t.Fatal(err)
			}
			ctx.Config().ApplyYAML(common.GetAppNamespace().Name(), string(content))

			// Wait until there is agent metrics for wasm download failure
			retry.UntilSuccessOrFail(t, func() error {
				q := "istio_agent_wasm_remote_fetch_count{result=\"download_failure\"}"
				c := cltInstance.Config().Cluster
				if _, err := common.QueryPrometheus(t, c, q, common.GetPromInstance()); err != nil {
					t.Logf("prometheus values for istio_agent_wasm_remote_fetch_count for cluster %v: \n%s",
						c, util.PromDump(c, common.GetPromInstance(), "istio_agent_wasm_remote_fetch_count"))
					return err
				}
				return nil
			}, retry.Delay(1*time.Second), retry.Timeout(80*time.Second))

			t.Log("got istio_agent_wasm_remote_fetch_count metric in prometheus, bad wasm filter is applied, send request to echo server again.")

			// Verify that istiod has a stats about rejected ECDS update
			// pilot_total_xds_rejects{type="type.googleapis.com/envoy.config.core.v3.TypedExtensionConfig"}
			retry.UntilSuccessOrFail(t, func() error {
				q := fmt.Sprintf("pilot_total_xds_rejects{type=\"%v\"}", resource.ExtensionConfigurationType)
				c := cltInstance.Config().Cluster
				if _, err := common.QueryPrometheus(t, c, q, common.GetPromInstance()); err != nil {
					t.Logf("prometheus values for pilot_total_xds_rejects for cluster %v: \n%s",
						c, util.PromDump(c, common.GetPromInstance(), "pilot_total_xds_rejects"))
					return err
				}
				return nil
			}, retry.Delay(1*time.Second), retry.Timeout(80*time.Second))

			// Verify that echo server could still return 200
			retry.UntilSuccessOrFail(t, func() error {
				if err := common.SendTraffic(cltInstance); err != nil {
					return err
				}
				return nil
			}, retry.Delay(1*time.Millisecond), retry.Timeout(5*time.Second))

			t.Log("echo server still returns ok after bad wasm filter is applied.")
		})
}
