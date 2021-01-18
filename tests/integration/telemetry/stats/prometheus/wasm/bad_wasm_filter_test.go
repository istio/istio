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
	"context"
	"fmt"
	"io/ioutil"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
	resource "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/util/retry"
	util "istio.io/istio/tests/integration/telemetry"
	common "istio.io/istio/tests/integration/telemetry/stats/prometheus"
)

// TestBadWasmRemoteLoad tests that bad Wasm remote load configuration won't affect service.
func TestBadWasmRemoteLoad(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			g, _ := errgroup.WithContext(context.Background())
			for _, cltInstance := range common.GetClientInstances() {
				g.Go(func() error {
					// Verify that echo server could return 200
					err := retry.UntilSuccess(func() error {
						if err := common.SendTraffic(t, cltInstance); err != nil {
							return err
						}
						return nil
					}, retry.Delay(10*time.Millisecond), retry.Timeout(5*time.Second))
					if err != nil {
						return err
					}
					t.Log("echo server returns OK, apply bad wasm remote load filter.")

					// Apply bad filter config
					content, err := ioutil.ReadFile("testdata/bad-filter.yaml")
					if err != nil {
						return err
					}
					if err := ctx.Config().ApplyYAML(common.GetAppNamespace().Name(), string(content)); err != nil {
						return err
					}

					// Wait until there is agent metrics for wasm download failure
					err = retry.UntilSuccess(func() error {
						q := "istio_agent_wasm_remote_fetch_count{result=\"download_failure\"}"
						c := cltInstance.Config().Cluster
						if _, err := common.QueryPrometheus(t, c, q, common.GetPromInstance()); err != nil {
							t.Logf("prometheus values for istio_agent_wasm_remote_fetch_count for cluster %v: \n%s",
								c, util.PromDump(c, common.GetPromInstance(), "istio_agent_wasm_remote_fetch_count"))
							return err
						}
						return nil
					}, retry.Delay(1*time.Second), retry.Timeout(40*time.Second))
					if err != nil {
						return err
					}

					t.Log("got istio_agent_wasm_remote_fetch_count metric in prometheus, bad wasm filter is applied, send request to echo server again.")

					// Verify that istiod has a stats about rejected ECDS update
					// pilot_total_xds_rejects{type="type.googleapis.com/envoy.config.core.v3.TypedExtensionConfig"}
					err = retry.UntilSuccess(func() error {
						q := fmt.Sprintf("pilot_total_xds_rejects{type=\"%v\"}", resource.ExtensionConfigurationType)
						c := cltInstance.Config().Cluster
						if _, err := common.QueryPrometheus(t, c, q, common.GetPromInstance()); err != nil {
							t.Logf("prometheus values for pilot_total_xds_rejects for cluster %v: \n%s",
								c, util.PromDump(c, common.GetPromInstance(), "pilot_total_xds_rejects"))
							return err
						}
						return nil
					}, retry.Delay(1*time.Second), retry.Timeout(40*time.Second))
					if err != nil {
						return err
					}

					// Verify that echo server could still return 200
					err = retry.UntilSuccess(func() error {
						if err := common.SendTraffic(t, cltInstance); err != nil {
							return err
						}
						return nil
					}, retry.Delay(10*time.Millisecond), retry.Timeout(5*time.Second))
					if err != nil {
						return err
					}

					t.Log("echo server still returns ok after bad wasm filter is applied.")
					return nil
				})
			}
			if err := g.Wait(); err != nil {
				t.Fatalf("test failed: %v", err)
			}
		})
}
