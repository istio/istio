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
	"strings"
	"testing"

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	tracingcfg "github.com/envoyproxy/go-control-plane/envoy/config/trace/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/tests/integration/telemetry/tracing"
)

// TestZipkinConfigDump verifies that the Zipkin tracing configuration from MeshConfig
// is correctly converted to Envoy configuration in the proxy config dump.
func TestZipkinConfigDump(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			// Get server instances from the tracing package
			serverInstances := tracing.GetServerInstances()

			if len(serverInstances) == 0 {
				ctx.Fatal("No server instances found")
			}

			server := serverInstances[0]
			workload := server.WorkloadsOrFail(ctx)[0]
			configDump := workload.Sidecar().ConfigOrFail(ctx)

			// Verify tracing configuration exists and has Zipkin provider
			verifyZipkinTracingExists(ctx, configDump)
		})
}

func verifyZipkinTracingExists(t framework.TestContext, configDump *admin.ConfigDump) {
	t.Helper()

	foundZipkinTracing := false
	var zipkinConfig *tracingcfg.ZipkinConfig

	// Find Zipkin tracing configuration
	for _, config := range configDump.Configs {
		if config.GetTypeUrl() != "type.googleapis.com/envoy.admin.v3.ListenersConfigDump" {
			continue
		}

		listenersDump := &admin.ListenersConfigDump{}
		if err := config.UnmarshalTo(listenersDump); err != nil {
			continue
		}

		// Check dynamic listeners
		for _, dynamicListener := range listenersDump.DynamicListeners {
			if dynamicListener.ActiveState == nil || dynamicListener.ActiveState.Listener == nil {
				continue
			}

			// Unmarshal the listener
			listenerConfig := &listener.Listener{}
			if err := dynamicListener.ActiveState.Listener.UnmarshalTo(listenerConfig); err != nil {
				continue
			}

			// Check filter chains
			for _, fc := range listenerConfig.FilterChains {
				for _, filter := range fc.Filters {
					if filter.Name != "envoy.filters.network.http_connection_manager" {
						continue
					}

					// Unmarshal the HCM config
					hcmConfig := &hcm.HttpConnectionManager{}
					if err := filter.GetTypedConfig().UnmarshalTo(hcmConfig); err != nil {
						continue
					}

					// Check if tracing is configured
					if hcmConfig.Tracing == nil || hcmConfig.Tracing.Provider == nil {
						continue
					}

					providerName := hcmConfig.Tracing.Provider.Name
					if !strings.Contains(providerName, "zipkin") {
						continue
					}

					// Found Zipkin! Try to unmarshal the config
					zipkinConfig = &tracingcfg.ZipkinConfig{}
					if err := hcmConfig.Tracing.Provider.GetTypedConfig().UnmarshalTo(zipkinConfig); err != nil {
						continue
					}

					foundZipkinTracing = true
					t.Logf("Found Zipkin tracing in listener: %s", dynamicListener.Name)
					t.Logf("Provider: %s", providerName)

					// Verify TraceContextOption
					if zipkinConfig.TraceContextOption != tracingcfg.ZipkinConfig_USE_B3_WITH_W3C_PROPAGATION {
						t.Fatalf("TraceContextOption mismatch: got %v, want USE_B3_WITH_W3C_PROPAGATION", zipkinConfig.TraceContextOption)
					}
					t.Log("TraceContextOption: USE_B3_WITH_W3C_PROPAGATION")

					// Verify HttpService with timeout and headers
					httpService := zipkinConfig.GetCollectorService()
					if httpService == nil {
						t.Fatal("HttpService is nil - timeout and headers not configured")
					}
					t.Log("Using HttpService (supports timeout/headers)")

					// Verify timeout
					if httpService.HttpUri == nil || httpService.HttpUri.Timeout == nil {
						t.Fatal("Timeout not configured")
					}

					timeout := httpService.HttpUri.Timeout.AsDuration()
					t.Logf("Timeout: %s", timeout)
					if timeout.String() != "10s" {
						t.Fatalf("Timeout mismatch: got %s, want 10s", timeout)
					}

					// Verify headers
					if len(httpService.RequestHeadersToAdd) == 0 {
						t.Fatal("No custom headers configured")
					}
					t.Logf("Custom headers: %d", len(httpService.RequestHeadersToAdd))

					// Check for specific headers
					foundCustomHeader := false
					foundAuthHeader := false
					for _, header := range httpService.RequestHeadersToAdd {
						t.Logf("  %s: %s", header.Header.Key, header.Header.Value)
						if header.Header.Key == "X-Custom-Header" && header.Header.Value == "test-value" {
							foundCustomHeader = true
						}
						if header.Header.Key == "Authorization" && header.Header.Value == "Bearer test-token" {
							foundAuthHeader = true
						}
					}

					if !foundCustomHeader {
						t.Fatal("X-Custom-Header not found")
					}
					t.Log("X-Custom-Header: test-value")

					if !foundAuthHeader {
						t.Fatal("Authorization header not found")
					}
					t.Log("Authorization: Bearer test-token")

					// Log sampling
					if hcmConfig.Tracing.RandomSampling != nil {
						t.Logf("Sampling: %.0f%%", hcmConfig.Tracing.RandomSampling.Value)
					}

					break
				}
				if foundZipkinTracing {
					break
				}
			}
			if foundZipkinTracing {
				break
			}
		}
		if foundZipkinTracing {
			break
		}
	}

	if !foundZipkinTracing {
		t.Fatal("No Zipkin tracing configuration found in any HTTP connection manager")
	}

	t.Log("Zipkin tracing configuration verified successfully")
}

// TestZipkinConfigWithDefaultProvider tests that extensionProviders with defaultProviders
// generates Envoy tracing configuration without requiring a Telemetry resource.
func TestZipkinConfigWithDefaultProvider(t *testing.T) {
	// This test verifies Option 1: defaultProviders.tracing: [zipkin]
	// The current setupConfig in main_test.go uses this approach

	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			// Get server instances from the tracing package
			serverInstances := tracing.GetServerInstances()

			if len(serverInstances) == 0 {
				ctx.Fatal("No server instances found")
			}

			server := serverInstances[0]
			workload := server.WorkloadsOrFail(ctx)[0]
			configDump := workload.Sidecar().ConfigOrFail(ctx)

			// Verify tracing is configured even without a Telemetry resource
			// This proves that extensionProviders + defaultProviders generates Envoy config
			foundTracing := false

			for _, config := range configDump.Configs {
				if config.GetTypeUrl() == "type.googleapis.com/envoy.admin.v3.ListenersConfigDump" {
					listenersDump := &admin.ListenersConfigDump{}
					if err := config.UnmarshalTo(listenersDump); err != nil {
						continue
					}

					for _, dynamicListener := range listenersDump.DynamicListeners {
						if dynamicListener.ActiveState == nil || dynamicListener.ActiveState.Listener == nil {
							continue
						}

						// Unmarshal the listener
						listenerConfig := &listener.Listener{}
						if err := dynamicListener.ActiveState.Listener.UnmarshalTo(listenerConfig); err != nil {
							continue
						}

						for _, fc := range listenerConfig.FilterChains {
							for _, filter := range fc.Filters {
								if filter.Name != "envoy.filters.network.http_connection_manager" {
									continue
								}

								hcmConfig := &hcm.HttpConnectionManager{}
								if err := filter.GetTypedConfig().UnmarshalTo(hcmConfig); err != nil {
									continue
								}

								if hcmConfig.Tracing != nil && hcmConfig.Tracing.Provider != nil {
									foundTracing = true
									ctx.Logf("Found tracing provider: %s", hcmConfig.Tracing.Provider.Name)
									break
								}
							}
							if foundTracing {
								break
							}
						}
						if foundTracing {
							break
						}
					}
					if foundTracing {
						break
					}
				}
			}

			if !foundTracing {
				ctx.Fatal("No tracing configuration found - extensionProviders + defaultProviders did NOT generate Envoy config")
			}

			ctx.Log("Confirmed: extensionProviders with defaultProviders generates Envoy tracing configuration")
		})
}
