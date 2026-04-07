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

package api

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
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

func applyLuaTrafficExtensionConfig(ctx framework.TestContext, ns string, args map[string]any, path string) error {
	return ctx.ConfigIstio().EvalFile(ns, args, path).Apply()
}

func installLuaTrafficExtension(ctx framework.TestContext, filterName, targetAppName, path string) error {
	args := map[string]any{
		"TrafficExtensionName": filterName,
		"TargetAppName":        targetAppName,
	}

	if err := applyLuaTrafficExtensionConfig(ctx, apps.Namespace.Name(), args, path); err != nil {
		return err
	}

	return nil
}

func uninstallLuaTrafficExtension(ctx framework.TestContext, filterName, path string) error {
	args := map[string]any{
		"TrafficExtensionName": filterName,
	}
	if err := ctx.ConfigIstio().EvalFile(apps.Namespace.Name(), args, path).Delete(); err != nil {
		return err
	}
	return nil
}

func resetLuaTrafficExtension(ctx framework.TestContext, filterName, path string, headerToCheck string) {
	ctx.NewSubTest("Delete TrafficExtension " + filterName).Run(func(t framework.TestContext) {
		if err := uninstallLuaTrafficExtension(t, filterName, path); err != nil {
			t.Fatal(err)
		}
		// Verify the header is no longer present
		sendTraffic(t, check.ResponseHeader(headerToCheck, ""), retry.Converge(2))
	})
}

// TestLuaTrafficExtension_HeaderInjection tests that Lua filters can inject headers in sidecar mode
func TestLuaTrafficExtension_HeaderInjection(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			filterName := "lua-header-injector"
			targetAppName := GetTarget().(echo.Instances).NamespacedName().Name

			// Install the header injector filter
			if err := installLuaTrafficExtension(t, filterName, targetAppName, luaHeaderInjectorFile); err != nil {
				t.Fatalf("failed to install TrafficExtension: %v", err)
			}

			// Verify the header is injected
			sendTraffic(t, check.ResponseHeader(luaInjectedHeader, "true"))

			// Cleanup
			resetLuaTrafficExtension(t, filterName, luaHeaderInjectorFile, luaInjectedHeader)
		})
}

// TestLuaTrafficExtension_ResponseModification tests that Lua filters can modify responses in sidecar mode
func TestLuaTrafficExtension_ResponseModification(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			filterName := "lua-response-modifier"
			targetAppName := GetTarget().(echo.Instances).NamespacedName().Name

			// Install the response modifier filter
			if err := installLuaTrafficExtension(t, filterName, targetAppName, luaResponseModifierFile); err != nil {
				t.Fatalf("failed to install TrafficExtension: %v", err)
			}

			// Verify the response header is added
			sendTraffic(t, check.ResponseHeader(luaResponseHeader, "modified"))

			// Cleanup
			resetLuaTrafficExtension(t, filterName, luaResponseModifierFile, luaResponseHeader)
		})
}

// TestLuaTrafficExtension_MultipleFilters tests multiple Lua filters with different priorities
func TestLuaTrafficExtension_MultipleFilters(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			targetAppName := GetTarget().(echo.Instances).NamespacedName().Name

			// Install header injector (phase: AUTHN)
			if err := installLuaTrafficExtension(t, "lua-header-injector", targetAppName, luaHeaderInjectorFile); err != nil {
				t.Fatalf("failed to install header injector: %v", err)
			}

			// Install response modifier (phase: STATS)
			if err := installLuaTrafficExtension(t, "lua-response-modifier", targetAppName, luaResponseModifierFile); err != nil {
				t.Fatalf("failed to install response modifier: %v", err)
			}

			// Verify both filters are active
			sendTraffic(t, check.And(
				check.ResponseHeader(luaInjectedHeader, "true"),
				check.ResponseHeader(luaResponseHeader, "modified"),
			))

			// Cleanup both filters
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

// TestLuaTrafficExtension_PhaseOrdering tests that filters execute in the correct phase order
func TestLuaTrafficExtension_PhaseOrdering(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			targetAppName := GetTarget().(echo.Instances).NamespacedName().Name

			// Install 3 filters, one for each phase. Each appends its phase number to
			// x-phase-order in envoy_on_response. Since response filters run in reverse
			// phase order (STATS -> AUTHZ -> AUTHN), the result should be "321".

			if err := installLuaTrafficExtension(t, "lua-phase-test-authn", targetAppName, "testdata/lua-phase-authn.yaml"); err != nil {
				t.Fatalf("failed to install AUTHN filter: %v", err)
			}

			if err := installLuaTrafficExtension(t, "lua-phase-test-authz", targetAppName, "testdata/lua-phase-authz.yaml"); err != nil {
				t.Fatalf("failed to install AUTHZ filter: %v", err)
			}

			if err := installLuaTrafficExtension(t, "lua-phase-test-stats", targetAppName, "testdata/lua-phase-stats.yaml"); err != nil {
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

// TestLuaTrafficExtension_SelectorMatching tests that selector-based matching works correctly
func TestLuaTrafficExtension_SelectorMatching(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			targetAppName := GetTarget().(echo.Instances).NamespacedName().Name

			// Install filter targeting specific app
			if err := installLuaTrafficExtension(t, "lua-selector-test", targetAppName, luaHeaderInjectorFile); err != nil {
				t.Fatalf("failed to install TrafficExtension: %v", err)
			}

			// Traffic to the target app should have the header
			sendTraffic(t, check.ResponseHeader(luaInjectedHeader, "true"))

			// Cleanup
			resetLuaTrafficExtension(t, "lua-selector-test", luaHeaderInjectorFile, luaInjectedHeader)
		})
}
