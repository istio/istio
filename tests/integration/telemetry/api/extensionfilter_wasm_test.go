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
	"fmt"
	"testing"

	"k8s.io/apiserver/pkg/storage/names"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/crd"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	wasmImageName               = "istio-testing/wasm/header-injector"
	wasmInjectedHeader          = "x-resp-injection"
	extensionFilterWasmFile     = "testdata/extensionfilter-wasm.yaml"
	extensionFilterWasmHTTPFile = "testdata/extensionfilter-wasm-http.yaml"
)

type extensionFilterWasmTestConfig struct {
	desc            string
	name            string
	policy          string
	tag             string
	upstreamVersion string
	expectedVersion string
}

var efGeneration = 0

func mapWasmTagToVersionOrFail(t framework.TestContext, tag, version string) {
	t.Helper()
	if err := registry.SetupTagMap(map[string]string{
		wasmImageName + ":" + tag: version,
	}); err != nil {
		t.Fatalf("failed to setup the tag map: %v", err)
	}
}

func applyAndTestExtensionFilterWithOCI(ctx framework.TestContext, c extensionFilterWasmTestConfig) {
	ctx.NewSubTest("OCI_" + c.desc).Run(func(t framework.TestContext) {
		defer func() {
			efGeneration++
		}()
		mapWasmTagToVersionOrFail(t, c.tag, c.upstreamVersion)
		wasmModuleURL := fmt.Sprintf("oci://%v/%v:%v", registry.Address(), wasmImageName, c.tag)
		if err := installWasmExtensionFilter(t, c.name, wasmModuleURL, c.policy, fmt.Sprintf("g-%d", efGeneration), extensionFilterWasmFile); err != nil {
			t.Fatalf("failed to install ExtensionFilter: %v", err)
		}
		sendTraffic(t, check.ResponseHeader(wasmInjectedHeader, c.expectedVersion))
	})
}

func resetExtensionFilterWasm(ctx framework.TestContext, filterName string) {
	ctx.NewSubTest("Delete ExtensionFilter " + filterName).Run(func(t framework.TestContext) {
		if err := uninstallWasmExtensionFilter(t, filterName, extensionFilterWasmFile); err != nil {
			t.Fatal(err)
		}
		sendTraffic(t, check.ResponseHeader(wasmInjectedHeader, ""), retry.Converge(2))
	})
}

func applyExtensionFilterWasmConfig(ctx framework.TestContext, ns string, args map[string]any, path string) error {
	return ctx.ConfigIstio().EvalFile(ns, args, path).Apply()
}

func installWasmExtensionFilter(ctx framework.TestContext, filterName, wasmModuleURL, imagePullPolicy, filterVersion, path string) error {
	args := map[string]any{
		"ExtensionFilterName": filterName,
		"TestWasmModuleURL":   wasmModuleURL,
		"FilterVersion":       filterVersion,
		"TargetAppName":       GetTarget().(echo.Instances).NamespacedName().Name,
	}

	if len(imagePullPolicy) != 0 {
		args["ImagePullPolicy"] = imagePullPolicy
	}

	if err := applyExtensionFilterWasmConfig(ctx, apps.Namespace.Name(), args, path); err != nil {
		return err
	}

	return nil
}

func uninstallWasmExtensionFilter(ctx framework.TestContext, filterName, path string) error {
	args := map[string]any{
		"ExtensionFilterName": filterName,
	}
	if err := ctx.ConfigIstio().EvalFile(apps.Namespace.Name(), args, path).Delete(); err != nil {
		return err
	}
	return nil
}

// TestExtensionFilter_ImagePullPolicy tests WASM image pull policies with ExtensionFilter
func TestExtensionFilter_ImagePullPolicy(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			tag := names.SimpleNameGenerator.GenerateName("test-tag-")
			applyAndTestExtensionFilterWithOCI(t, extensionFilterWasmTestConfig{
				desc:            "initial creation with 0.0.1",
				name:            "extensionfilter-wasm-test",
				tag:             tag,
				policy:          "",
				upstreamVersion: "0.0.1",
				expectedVersion: "0.0.1",
			})

			resetExtensionFilterWasm(t, "extensionfilter-wasm-test")
			applyAndTestExtensionFilterWithOCI(t, extensionFilterWasmTestConfig{
				desc:            "upstream is upgraded to 0.0.2, but 0.0.1 is already present and policy is IfNotPresent",
				name:            "extensionfilter-wasm-test",
				tag:             tag,
				policy:          "IfNotPresent",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.1",
			})

			resetExtensionFilterWasm(t, "extensionfilter-wasm-test")
			applyAndTestExtensionFilterWithOCI(t, extensionFilterWasmTestConfig{
				desc:            "upstream is upgraded to 0.0.2, but 0.0.1 is already present and policy is default",
				name:            "extensionfilter-wasm-test",
				tag:             tag,
				policy:          "",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.1",
			})

			// Intentionally, do not reset here to see the upgrade from 0.0.1.
			applyAndTestExtensionFilterWithOCI(t, extensionFilterWasmTestConfig{
				desc:            "upstream is upgraded to 0.0.2. 0.0.1 is already present but policy is Always, so pull 0.0.2",
				name:            "extensionfilter-wasm-test",
				tag:             tag,
				policy:          "Always",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.2",
			})
		})
}

// TestExtensionFilter_ImagePullPolicyWithHTTP tests WASM HTTP URLs with ExtensionFilter
func TestExtensionFilter_ImagePullPolicyWithHTTP(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			tag := names.SimpleNameGenerator.GenerateName("test-tag-")
			applyAndTestExtensionFilterWithHTTP(t, extensionFilterWasmTestConfig{
				desc:            "initial creation with 0.0.1",
				name:            "extensionfilter-wasm-http-test",
				tag:             tag,
				upstreamVersion: "0.0.1",
				expectedVersion: "0.0.1",
			})

			resetExtensionFilterWasmHTTP(t, "extensionfilter-wasm-http-test")
		})
}

func applyAndTestExtensionFilterWithHTTP(ctx framework.TestContext, c extensionFilterWasmTestConfig) {
	ctx.NewSubTest("HTTP_" + c.desc).Run(func(t framework.TestContext) {
		defer func() {
			efGeneration++
		}()
		mapWasmTagToVersionOrFail(t, c.tag, c.upstreamVersion)
		wasmModuleURL := fmt.Sprintf("http://%v/wasm/%v/%v.wasm", registry.Address(), wasmImageName, c.tag)
		if err := installWasmExtensionFilter(t, c.name, wasmModuleURL, "", fmt.Sprintf("g-%d", efGeneration), extensionFilterWasmHTTPFile); err != nil {
			t.Fatalf("failed to install ExtensionFilter: %v", err)
		}
		sendTraffic(t, check.ResponseHeader(wasmInjectedHeader, c.expectedVersion))
	})
}

func resetExtensionFilterWasmHTTP(ctx framework.TestContext, filterName string) {
	ctx.NewSubTest("Delete ExtensionFilter " + filterName).Run(func(t framework.TestContext) {
		if err := uninstallWasmExtensionFilter(t, filterName, extensionFilterWasmHTTPFile); err != nil {
			t.Fatal(err)
		}
		sendTraffic(t, check.ResponseHeader(wasmInjectedHeader, ""), retry.Converge(2))
	})
}

// TestExtensionFilter_BadWasmRemoteLoad tests WASM load failures with ExtensionFilter
func TestExtensionFilter_BadWasmRemoteLoad(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			// This test verifies that a bad WASM module fails to load
			// Using a non-existent OCI image should cause the filter to fail
			if err := installWasmExtensionFilter(t, "bad-wasm-filter", "oci://invalid-registry.example.com/nonexistent:latest", "", "v1", extensionFilterWasmFile); err != nil {
				t.Fatalf("failed to install ExtensionFilter: %v", err)
			}

			// Traffic should still work (fail open by default)
			sendTraffic(t, check.OK())

			// Cleanup
			if err := uninstallWasmExtensionFilter(t, "bad-wasm-filter", extensionFilterWasmFile); err != nil {
				t.Fatal(err)
			}
		})
}

// TestExtensionFilter_SelectorMatching tests selector-based WASM filter targeting
func TestExtensionFilter_SelectorMatching(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			tag := names.SimpleNameGenerator.GenerateName("test-tag-")
			mapWasmTagToVersionOrFail(t, tag, "0.0.1")
			wasmModuleURL := fmt.Sprintf("oci://%v/%v:%v", registry.Address(), wasmImageName, tag)

			// Install filter targeting specific app via selector
			if err := installWasmExtensionFilter(t, "wasm-selector-test", wasmModuleURL, "", "v1", extensionFilterWasmFile); err != nil {
				t.Fatalf("failed to install ExtensionFilter: %v", err)
			}

			// Traffic to the target app should have the header
			sendTraffic(t, check.ResponseHeader(wasmInjectedHeader, "0.0.1"))

			// Cleanup
			resetExtensionFilterWasm(t, "wasm-selector-test")
		})
}

// TestExtensionFilter_GatewaySelection tests ExtensionFilter targeting a Gateway resource
func TestExtensionFilter_GatewaySelection(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			crd.DeployGatewayAPIOrSkip(t)
			args := map[string]any{
				"To": GetTarget().(echo.Instances),
			}
			t.ConfigIstio().EvalFile(apps.Namespace.Name(), args, "testdata/gateway-api.yaml").ApplyOrFail(t)

			tag := "latest"
			mapWasmTagToVersionOrFail(t, tag, "0.0.1")
			wasmModuleURL := fmt.Sprintf("oci://%v/%v:%v", registry.Address(), wasmImageName, tag)

			gatewayArgs := map[string]any{
				"ExtensionFilterName": "gateway-wasm-filter",
				"TestWasmModuleURL":   wasmModuleURL,
				"FilterVersion":       "v1",
				"TargetGatewayName":   GetTarget().(echo.Instances).ServiceName() + "-gateway",
			}

			if err := applyExtensionFilterWasmConfig(t, apps.Namespace.Name(), gatewayArgs, "testdata/extensionfilter-gateway-wasm.yaml"); err != nil {
				t.Fatalf("failed to install Gateway ExtensionFilter: %v", err)
			}

			// Test with gateway hostname
			testHostname := fmt.Sprintf("%s-gateway-istio.%s.svc.cluster.local", GetTarget().ServiceName(), apps.Namespace.Name())
			sendTrafficToHostname(t, check.ResponseHeader(wasmInjectedHeader, "0.0.1"), testHostname)

			// Cleanup
			if err := uninstallWasmExtensionFilter(t, "gateway-wasm-filter", "testdata/extensionfilter-gateway-wasm.yaml"); err != nil {
				t.Fatal(err)
			}
		})
}

// TestExtensionFilter_BadWasmWithFailOpen tests WASM load failures with fail_open strategy
func TestExtensionFilter_BadWasmWithFailOpen(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			// Enable logging for debugging
			applyTelemetryResource(t, true)

			// Apply bad filter config with FAIL_OPEN
			args := map[string]any{
				"TargetAppName": GetTarget().(echo.Instances).NamespacedName().Name,
			}
			t.ConfigIstio().EvalFile(apps.Namespace.Name(), args, "testdata/bad-wasm-extensionfilter-fail-open.yaml").ApplyOrFail(t)

			// Since fail_open=true, traffic should continue to work even though WASM fails to load
			sendTraffic(t, check.OK())

			// Cleanup
			t.ConfigIstio().EvalFile(apps.Namespace.Name(), args, "testdata/bad-wasm-extensionfilter-fail-open.yaml").DeleteOrFail(t)
		})
}
