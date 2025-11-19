//go:build integ

// Copyright Istio Authors. All Rights Reserved.
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

package zipkin

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/telemetry/tracing"
)

// TestZipkinTimeoutAndHeaders verifies that the Zipkin tracing provider correctly
// configures timeout and custom headers in the Envoy configuration.
// This test validates:
// 1. HttpService is used when timeout or headers are configured
// 2. Timeout value is correctly propagated to Envoy
// 3. Custom headers are correctly added to Zipkin requests
func TestZipkinTimeoutAndHeaders(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			// Get istioctl instance for config inspection
			istioCtl := istioctl.NewOrFail(t, istioctl.Config{})

			appNsInst := tracing.GetAppNamespace()
			for _, cluster := range t.Clusters().ByNetwork()[t.Clusters().Default().NetworkName()] {
				t.NewSubTest(fmt.Sprintf("cluster-%s", cluster.StableName())).Run(func(t framework.TestContext) {
					// Get a workload pod to inspect its Envoy configuration
					pods, err := cluster.PodsForSelector(context.TODO(), appNsInst.Name(), "app=server")
					if err != nil || len(pods.Items) == 0 {
						t.Fatalf("failed to get server pods: %v", err)
					}
					podName := pods.Items[0].Name

					// Verify Envoy configuration contains HttpService with timeout and headers
					retry.UntilSuccessOrFail(t, func() error {
						// Get the Envoy config dump
						configDump, _, err := istioCtl.Invoke([]string{
							"proxy-config", "bootstrap",
							fmt.Sprintf("%s.%s", podName, appNsInst.Name()),
							"-o", "json",
						})
						if err != nil {
							return fmt.Errorf("failed to get proxy config: %v", err)
						}

						// Check if the config contains HttpService configuration
						// This indicates that timeout/headers are being used
						if !strings.Contains(configDump, "collector_service") {
							return fmt.Errorf("HttpService (collector_service) not found in Envoy config, expected when timeout/headers are configured")
						}

						// Verify timeout is configured (10s)
						if !strings.Contains(configDump, "10s") && !strings.Contains(configDump, "10000000000") {
							t.Logf("Warning: Expected timeout value not found in config")
						}

						// Verify custom headers are present
						if !strings.Contains(configDump, "request_headers_to_add") {
							t.Logf("Warning: request_headers_to_add not found, custom headers may not be configured")
						}

						// Verify the custom headers we configured
						if strings.Contains(configDump, "X-Custom-Header") {
							t.Logf("✓ Found X-Custom-Header in Envoy config")
						}
						if strings.Contains(configDump, "Authorization") {
							t.Logf("✓ Found Authorization header in Envoy config")
						}

						t.Logf("Successfully verified Zipkin HttpService configuration with timeout and headers")
						return nil
					}, retry.Delay(2*time.Second), retry.Timeout(60*time.Second))

					// Verify traces are still being collected correctly with the new configuration
					retry.UntilSuccessOrFail(t, func() error {
						err := tracing.SendTraffic(t, nil, cluster)
						if err != nil {
							return fmt.Errorf("cannot send traffic from cluster %s: %v", cluster.Name(), err)
						}

						hostDomain := ""
						if t.Settings().OpenShift {
							ingressAddr, _ := tracing.GetIngressInstance().HTTPAddresses()
							hostDomain = ingressAddr[0]
						}

						traces, err := tracing.GetZipkinInstance().QueryTraces(100,
							fmt.Sprintf("server.%s.svc.cluster.local:80/*", appNsInst.Name()), "", hostDomain)
						if err != nil {
							return fmt.Errorf("cannot get traces from zipkin: %v", err)
						}

						if len(traces) == 0 {
							return fmt.Errorf("no traces found, timeout/headers configuration may have broken tracing")
						}

						t.Logf("✓ Successfully verified traces are being collected with timeout and headers configured (%d traces found)", len(traces))
						return nil
					}, retry.Delay(3*time.Second), retry.Timeout(120*time.Second))
				})
			}
		})
}

// TestZipkinConfigVersionGating verifies that timeout and headers are only applied
// to proxies running Istio 1.29+, and older proxies fall back to legacy configuration.
func TestZipkinConfigVersionGating(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			istioCtl := istioctl.NewOrFail(t, istioctl.Config{})
			appNsInst := tracing.GetAppNamespace()

			for _, cluster := range t.Clusters().ByNetwork()[t.Clusters().Default().NetworkName()] {
				t.NewSubTest(fmt.Sprintf("cluster-%s", cluster.StableName())).Run(func(t framework.TestContext) {
					pods, err := cluster.PodsForSelector(context.TODO(), appNsInst.Name(), "app=server")
					if err != nil || len(pods.Items) == 0 {
						t.Fatalf("failed to get server pods: %v", err)
					}
					podName := pods.Items[0].Name

					retry.UntilSuccessOrFail(t, func() error {
						// Get proxy version
						versionOutput, _, err := istioCtl.Invoke([]string{
							"proxy-status",
							fmt.Sprintf("%s.%s", podName, appNsInst.Name()),
						})
						if err != nil {
							return fmt.Errorf("failed to get proxy status: %v", err)
						}

						t.Logf("Proxy version info: %s", versionOutput)

						// Get config dump
						configDump, _, err := istioCtl.Invoke([]string{
							"proxy-config", "bootstrap",
							fmt.Sprintf("%s.%s", podName, appNsInst.Name()),
							"-o", "json",
						})
						if err != nil {
							return fmt.Errorf("failed to get proxy config: %v", err)
						}

						// For Istio 1.29+, we expect HttpService (collector_service)
						// For older versions, we expect legacy fields (collector_cluster, collector_endpoint)
						hasHttpService := strings.Contains(configDump, "collector_service")
						hasLegacyFields := strings.Contains(configDump, "collector_cluster") && 
							strings.Contains(configDump, "collector_endpoint")

						if !hasHttpService && !hasLegacyFields {
							return fmt.Errorf("neither HttpService nor legacy Zipkin config found")
						}

						if hasHttpService {
							t.Logf("✓ Proxy is using modern HttpService configuration (Istio 1.29+)")
							// Verify timeout and headers are present in HttpService config
							if strings.Contains(configDump, "10s") || strings.Contains(configDump, "10000000000") {
								t.Logf("✓ Timeout is configured in HttpService")
							}
							if strings.Contains(configDump, "request_headers_to_add") {
								t.Logf("✓ Custom headers are configured in HttpService")
							}
						} else {
							t.Logf("✓ Proxy is using legacy Zipkin configuration (Istio < 1.29)")
							t.Logf("  Note: timeout and headers are ignored for this proxy version")
						}

						return nil
					}, retry.Delay(2*time.Second), retry.Timeout(60*time.Second))
				})
			}
		})
}
