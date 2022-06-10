//go:build integ
// +build integ

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

package wasm

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/util/retry"
	common "istio.io/istio/tests/integration/telemetry/stats/prometheus"
)

const (
	imageName      = "istio-testing/wasm/header-injector"
	injectedHeader = "x-resp-injection"
	wasmConfigFile = "testdata/wasm-filter.yaml"
)

type wasmTestConfigs struct {
	desc            string
	name            string
	policy          string
	tag             string
	upstreamVersion string
	expectedVersion string
}

var generation = 0

func mapTagToVersionOrFail(t framework.TestContext, tag, version string) {
	t.Helper()
	if err := registry.SetupTagMap(map[string]string{
		imageName + ":" + tag: version,
	}); err != nil {
		t.Fatalf("failed to setup the tag map: %v", err)
	}
}

func applyAndTestWasmWithOCI(ctx framework.TestContext, c wasmTestConfigs) {
	ctx.NewSubTest("OCI_" + c.desc).Run(func(t framework.TestContext) {
		defer func() {
			generation++
		}()
		mapTagToVersionOrFail(t, c.tag, c.upstreamVersion)
		wasmModuleURL := fmt.Sprintf("oci://%v/%v:%v", registry.Address(), imageName, c.tag)
		if err := installWasmExtension(t, c.name, wasmModuleURL, c.policy, fmt.Sprintf("g-%d", generation)); err != nil {
			t.Fatalf("failed to install WasmPlugin: %v", err)
		}
		sendTraffic(t, check.ResponseHeader(injectedHeader, c.expectedVersion))
	})
}

func resetWasm(ctx framework.TestContext, pluginName string) {
	ctx.NewSubTest("Delete WasmPlugin " + pluginName).Run(func(t framework.TestContext) {
		if err := uninstallWasmExtension(t, pluginName); err != nil {
			t.Fatal(err)
		}
		sendTraffic(t, check.ResponseHeader(injectedHeader, ""), retry.Converge(2))
	})
}

func TestImagePullPolicy(t *testing.T) {
	framework.NewTest(t).
		Features("extensibility.wasm.image-pull-policy").
		Features("extensibility.wasm.remote-load").
		Run(func(t framework.TestContext) {
			applyAndTestWasmWithOCI(t, wasmTestConfigs{
				desc:            "initial creation with latest",
				name:            "wasm-test-module",
				tag:             "latest",
				policy:          "",
				upstreamVersion: "0.0.1",
				expectedVersion: "0.0.1",
			})

			resetWasm(t, "wasm-test-module")
			applyAndTestWasmWithOCI(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2. 0.0.1 is already present and policy is IfNotPresent, so should not pull",
				name:            "wasm-test-module",
				tag:             "latest",
				policy:          "IfNotPresent",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.1",
			})

			// Intentionally, do not reset here to see the upgrade from 0.0.1.
			applyAndTestWasmWithOCI(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2. 0.0.1 is already present. But policy is default and tag is latest, so pull the image",
				name:            "wasm-test-module",
				tag:             "latest",
				policy:          "",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.2",
			})
			resetWasm(t, "wasm-test-module")

			applyAndTestWasmWithOCI(t, wasmTestConfigs{
				desc:            "initial creation with 0.0.1",
				name:            "wasm-test-module-test-tag-1",
				tag:             "test-tag-1",
				policy:          "",
				upstreamVersion: "0.0.1",
				expectedVersion: "0.0.1",
			})

			resetWasm(t, "wasm-test-module-test-tag-1")
			applyAndTestWasmWithOCI(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2, but 0.0.1 is already present and policy is IfNotPresent",
				name:            "wasm-test-module-test-tag-1",
				tag:             "test-tag-1",
				policy:          "IfNotPresent",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.1",
			})

			resetWasm(t, "wasm-test-module-test-tag-1")
			applyAndTestWasmWithOCI(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2, but 0.0.1 is already present and policy is default",
				name:            "wasm-test-module-test-tag-1",
				tag:             "test-tag-1",
				policy:          "",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.1",
			})

			// Intentionally, do not reset here to see the upgrade from 0.0.1.
			applyAndTestWasmWithOCI(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2. 0.0.1 is already present but policy is Always, so pull 0.0.2",
				name:            "wasm-test-module-test-tag-1",
				tag:             "test-tag-1",
				policy:          "Always",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.2",
			})
		})
}

func installWasmExtension(ctx framework.TestContext, pluginName, wasmModuleURL, imagePullPolicy, pluginVersion string) error {
	args := map[string]interface{}{
		"WasmPluginName":    pluginName,
		"TestWasmModuleURL": wasmModuleURL,
		"WasmPluginVersion": pluginVersion,
		"TargetAppName":     common.GetTarget().(echo.Instances).NamespacedName().Name,
	}

	if len(imagePullPolicy) != 0 {
		args["ImagePullPolicy"] = imagePullPolicy
	}

	if err := ctx.ConfigIstio().EvalFile(common.GetAppNamespace().Name(), args, wasmConfigFile).
		Apply(); err != nil {
		return err
	}

	return nil
}

func uninstallWasmExtension(ctx framework.TestContext, pluginName string) error {
	args := map[string]interface{}{
		"WasmPluginName": pluginName,
	}
	if err := ctx.ConfigIstio().EvalFile(common.GetAppNamespace().Name(), args, wasmConfigFile).Delete(); err != nil {
		return err
	}
	return nil
}

func sendTraffic(ctx framework.TestContext, checker echo.Checker, options ...retry.Option) {
	ctx.Helper()
	if len(common.GetClientInstances()) == 0 {
		ctx.Fatal("there is no client")
	}
	cltInstance := common.GetClientInstances()[0]

	defaultOptions := []retry.Option{retry.Delay(100 * time.Millisecond), retry.Timeout(200 * time.Second)}
	httpOpts := echo.CallOptions{
		To: common.GetTarget(),
		Port: echo.Port{
			Name: "http",
		},
		HTTP: echo.HTTP{
			Path:   "/path",
			Method: "GET",
		},
		Count: 1,
		Retry: echo.Retry{
			Options: append(defaultOptions, options...),
		},
		Check: checker,
	}

	_ = cltInstance.CallOrFail(ctx, httpOpts)
}

func applyAndTestWasmWithHTTP(ctx framework.TestContext, c wasmTestConfigs) {
	ctx.NewSubTest("HTTP_" + c.desc).Run(func(t framework.TestContext) {
		defer func() {
			generation++
		}()
		mapTagToVersionOrFail(t, c.tag, c.upstreamVersion)
		// registry-redirector will redirect to the gzipped tarball of the first layer with this request.
		// The gzipped tarball should have a wasm module.
		wasmModuleURL := fmt.Sprintf("http://%v/layer/v1/%v:%v", registry.Address(), imageName, c.tag)
		t.Logf("Trying to get a wasm file from %v", wasmModuleURL)
		if err := installWasmExtension(t, c.name, wasmModuleURL, c.policy, fmt.Sprintf("g-%d", generation)); err != nil {
			t.Fatalf("failed to install WasmPlugin: %v", err)
		}
		sendTraffic(t, check.ResponseHeader(injectedHeader, c.expectedVersion))
	})
}

// TestImagePullPolicyWithHTTP tests pulling Wasm Binary via HTTP and ImagePullPolicy.
func TestImagePullPolicyWithHTTP(t *testing.T) {
	framework.NewTest(t).
		Features("extensibility.wasm.image-pull-policy").
		Features("extensibility.wasm.remote-load").
		Run(func(t framework.TestContext) {
			applyAndTestWasmWithHTTP(t, wasmTestConfigs{
				desc:            "initial creation with 0.0.1",
				name:            "wasm-test-module-http",
				tag:             "test-tag-http",
				policy:          "",
				upstreamVersion: "0.0.1",
				expectedVersion: "0.0.1",
			})

			resetWasm(t, "wasm-test-module-http")
			applyAndTestWasmWithHTTP(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2, but 0.0.1 is already present and policy is IfNotPresent",
				name:            "wasm-test-module-http",
				tag:             "test-tag-http",
				policy:          "IfNotPresent",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.1",
			})

			resetWasm(t, "wasm-test-module-http")
			applyAndTestWasmWithHTTP(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2, but 0.0.1 is already present and policy is default",
				name:            "wasm-test-module-http",
				tag:             "test-tag-http",
				policy:          "",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.1",
			})

			// Intentionally, do not reset here to see the upgrade from 0.0.1.
			applyAndTestWasmWithHTTP(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2. 0.0.1 is already present but policy is Always, so pull 0.0.2",
				name:            "wasm-test-module-http",
				tag:             "test-tag-http",
				policy:          "Always",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.2",
			})
		})
}
