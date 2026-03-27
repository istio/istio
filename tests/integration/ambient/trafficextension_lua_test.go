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

package ambient

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/crd"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	luaInjectedHeader       = "x-lua-injected"
	luaResponseHeader       = "x-lua-response"
	luaHeaderInjectorFile   = "testdata/lua-header-injector.yaml"
	luaResponseModifierFile = "testdata/lua-response-modifier.yaml"
)

type luaTestConfig struct {
	desc         string
	name         string
	testHostname string
}

func applyAndTestLuaTrafficExtension(ctx framework.TestContext, c luaTestConfig, path, targetType, targetName, expectedHeader, expectedValue string) {
	ctx.NewSubTest(c.desc).Run(func(t framework.TestContext) {
		if err := installLuaTrafficExtension(t, c.name, targetType, targetName, path); err != nil {
			t.Fatalf("failed to install ExtensionFilter: %v", err)
		}
		if c.testHostname != "" {
			sendTrafficToHostname(t, check.ResponseHeader(expectedHeader, expectedValue), c.testHostname)
		} else {
			sendTraffic(t, check.ResponseHeader(expectedHeader, expectedValue))
		}
	})
}

func resetLuaTrafficExtension(ctx framework.TestContext, filterName, path string) {
	ctx.NewSubTest("Delete TrafficExtension " + filterName).Run(func(t framework.TestContext) {
		if err := uninstallLuaTrafficExtension(t, filterName, path); err != nil {
			t.Fatal(err)
		}
		// Verify the header is no longer present
		sendTraffic(t, check.ResponseHeader(luaInjectedHeader, ""), retry.Converge(2))
	})
}

func applyLuaTrafficExtensionConfig(ctx framework.TestContext, ns string, args map[string]any, path string) error {
	return ctx.ConfigIstio().EvalFile(ns, args, path).Apply()
}

func installLuaTrafficExtension(ctx framework.TestContext, filterName, targetType, targetName, path string) error {
	kind, group, name := getTargetRefValues(targetType, targetName)

	args := map[string]any{
		"ExtensionFilterName": filterName,
		"TargetKind":          kind,
		"TargetGroup":         group,
		"TargetName":          name,
	}

	if err := applyLuaTrafficExtensionConfig(ctx, apps.Namespace.Name(), args, path); err != nil {
		return err
	}

	return nil
}

func uninstallLuaTrafficExtension(ctx framework.TestContext, filterName, path string) error {
	args := map[string]any{
		"ExtensionFilterName": filterName,
	}
	if err := ctx.ConfigIstio().EvalFile(apps.Namespace.Name(), args, path).Delete(); err != nil {
		return err
	}
	return nil
}

// TestLuaTrafficExtension_HeaderInjection tests that Lua filters can inject headers
func TestLuaTrafficExtension_HeaderInjection(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			testCases := []struct {
				desc         string
				name         string
				testHostname string
				targetType   string
				targetName   string
			}{
				{
					desc:         "Lua filter for gateway",
					name:         "gateway-lua-test",
					testHostname: fmt.Sprintf("%s-gateway-istio.%s.svc.cluster.local", GetTarget().ServiceName(), apps.Namespace.Name()),
					targetType:   "gateway",
					targetName:   fmt.Sprintf("%s-gateway", GetTarget().(echo.Instances).ServiceName()),
				},
				{
					desc:       "Lua filter for waypoint",
					name:       "waypoint-lua-test",
					targetType: "gateway",
					targetName: constants.DefaultNamespaceWaypoint,
				},
				{
					desc:       "Lua filter for specific service",
					name:       "service-lua-test",
					targetType: "service",
					targetName: GetTarget().Instances().ServiceName(),
				},
			}

			for _, tc := range testCases {
				if tc.name == "gateway-lua-test" {
					crd.DeployGatewayAPIOrSkip(t)
					args := map[string]any{
						"To": GetTarget().Instances(),
					}
					t.ConfigIstio().EvalFile(apps.Namespace.Name(), args, "testdata/gateway-api.yaml").ApplyOrFail(t)
				}

				applyAndTestLuaTrafficExtension(t, luaTestConfig{
					desc:         tc.desc,
					name:         tc.name,
					testHostname: tc.testHostname,
				}, luaHeaderInjectorFile, tc.targetType, tc.targetName, luaInjectedHeader, "true")

				resetLuaTrafficExtension(t, tc.name, luaHeaderInjectorFile)
			}
		})
}

// TestLuaTrafficExtension_ResponseModification tests that Lua filters can modify responses
func TestLuaTrafficExtension_ResponseModification(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			filterName := "lua-response-modifier"
			targetType := "service"
			targetName := GetTarget().Instances().ServiceName()

			// Install the response modifier filter
			if err := installLuaTrafficExtension(t, filterName, targetType, targetName, luaResponseModifierFile); err != nil {
				t.Fatalf("failed to install ExtensionFilter: %v", err)
			}

			// Verify the response header is added
			sendTraffic(t, check.ResponseHeader(luaResponseHeader, "modified"))

			// Cleanup
			if err := uninstallLuaTrafficExtension(t, filterName, luaResponseModifierFile); err != nil {
				t.Fatal(err)
			}

			// Verify the header is no longer present
			sendTraffic(t, check.ResponseHeader(luaResponseHeader, ""), retry.Converge(2))
		})
}

// TestLuaTrafficExtension_MultipleFilters tests multiple Lua filters with different priorities
func TestLuaTrafficExtension_MultipleFilters(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			targetType := "service"
			targetName := GetTarget().Instances().ServiceName()

			// Install header injector (phase: AUTHN)
			if err := installLuaTrafficExtension(t, "lua-header-injector", targetType, targetName, luaHeaderInjectorFile); err != nil {
				t.Fatalf("failed to install header injector: %v", err)
			}

			// Install response modifier (phase: STATS)
			if err := installLuaTrafficExtension(t, "lua-response-modifier", targetType, targetName, luaResponseModifierFile); err != nil {
				t.Fatalf("failed to install response modifier: %v", err)
			}

			// Verify both filters are active
			sendTraffic(t, check.And(
				check.ResponseHeader(luaInjectedHeader, "true"),
				check.ResponseHeader(luaResponseHeader, "modified"),
			))

			// Cleanup
			if err := uninstallLuaTrafficExtension(t, "lua-header-injector", luaHeaderInjectorFile); err != nil {
				t.Fatal(err)
			}
			if err := uninstallLuaTrafficExtension(t, "lua-response-modifier", luaResponseModifierFile); err != nil {
				t.Fatal(err)
			}

			// Verify both headers are gone
			sendTraffic(t, check.And(
				check.ResponseHeader(luaInjectedHeader, ""),
				check.ResponseHeader(luaResponseHeader, ""),
			), retry.Converge(2))
		})
}

// TestLuaTrafficExtension_PhaseOrdering tests that filters execute in the correct phase order in ambient mode
func TestLuaTrafficExtension_PhaseOrdering(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			targetType := "service"
			targetName := GetTarget().Instances().ServiceName()

			// Install 3 filters, one for each phase. Each appends its phase number to
			// x-phase-order in envoy_on_response. Since response filters run in reverse
			// phase order (STATS -> AUTHZ -> AUTHN), the result should be "321".

			if err := installLuaTrafficExtension(t, "lua-phase-test-authn", targetType, targetName, "testdata/lua-phase-authn.yaml"); err != nil {
				t.Fatalf("failed to install AUTHN filter: %v", err)
			}

			if err := installLuaTrafficExtension(t, "lua-phase-test-authz", targetType, targetName, "testdata/lua-phase-authz.yaml"); err != nil {
				t.Fatalf("failed to install AUTHZ filter: %v", err)
			}

			if err := installLuaTrafficExtension(t, "lua-phase-test-stats", targetType, targetName, "testdata/lua-phase-stats.yaml"); err != nil {
				t.Fatalf("failed to install STATS filter: %v", err)
			}

			// On the response path filters execute in reverse phase order: STATS -> AUTHZ -> AUTHN
			// STATS appends "3", AUTHZ appends "2", AUTHN appends "1", yielding "321"
			sendTraffic(t, check.ResponseHeader("x-phase-order", "321"))

			// Cleanup all three filters
			if err := uninstallLuaTrafficExtension(t, "lua-phase-test-authn", "testdata/lua-phase-authn.yaml"); err != nil {
				t.Fatal(err)
			}
			if err := uninstallLuaTrafficExtension(t, "lua-phase-test-authz", "testdata/lua-phase-authz.yaml"); err != nil {
				t.Fatal(err)
			}
			if err := uninstallLuaTrafficExtension(t, "lua-phase-test-stats", "testdata/lua-phase-stats.yaml"); err != nil {
				t.Fatal(err)
			}

			// Verify the header is gone
			sendTraffic(t, check.ResponseHeader("x-phase-order", ""), retry.Converge(2))
		})
}
