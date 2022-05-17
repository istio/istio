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
	"errors"
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/scopes"
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

func applyAndTestWasm(t framework.TestContext, c wasmTestConfigs) {
	t.Helper()
	defer func() {
		generation++
	}()
	scopes.Framework.Infof(c.desc)
	mapTagToVersionOrFail(t, c.tag, c.upstreamVersion)
	if err := installWasmExtension(t, c.name, c.tag, c.policy, fmt.Sprintf("g-%q", generation)); err != nil {
		t.Fatalf("failed to install WasmPlugin: %v", err)
	}
	retry.UntilSuccessOrFail(t, func() error {
		result, err := sendTraffic()
		if err != nil {
			t.Log("failed to send traffic")
			return err
		}

		if result.Responses.Len() == 0 {
			return errors.New("failed to get response")
		}

		value := result.Responses[0].ResponseHeaders.Get(injectedHeader)
		if value != c.expectedVersion {
			return fmt.Errorf("[generation: %d] unexpected values for the header %q with the policy %q: given %q, got %q, want %q",
				generation, injectedHeader, c.policy, c.upstreamVersion, value, c.expectedVersion)
		}

		return nil
	}, retry.Delay(1*time.Second), retry.Timeout(100*time.Second))
}

func resetWasm(t framework.TestContext, pluginName string) {
	t.Helper()
	scopes.Framework.Infof("Delete WasmPlugin %q", pluginName)
	if err := uninstallWasmExtension(t, pluginName); err != nil {
		t.Fatal(err)
	}

	successCount := 0

	retry.UntilSuccessOrFail(t, func() error {
		result, err := sendTraffic()
		if err != nil {
			t.Log("failed to send traffic")
			return err
		}

		if result.Responses.Len() == 0 {
			return errors.New("failed to get response")
		}

		value := result.Responses[0].ResponseHeaders.Get(injectedHeader)

		if value != "" {
			return fmt.Errorf("failed to reset WasmPlugin %q", pluginName)
		}

		if successCount < 2 {
			successCount++
			return fmt.Errorf("retry")
		}
		return nil
	}, retry.Delay(1*time.Second), retry.Timeout(100*time.Second))
}

func TestImagePullPolicy(t *testing.T) {
	framework.NewTest(t).
		Features("extensibility.wasm.image-pull-policy").
		Features("extensibility.wasm.remote-load").
		Run(func(t framework.TestContext) {
			applyAndTestWasm(t, wasmTestConfigs{
				desc:            "initial creation with latest",
				name:            "wasm-test-module",
				tag:             "latest",
				policy:          "",
				upstreamVersion: "0.0.1",
				expectedVersion: "0.0.1",
			})

			resetWasm(t, "wasm-test-module")
			applyAndTestWasm(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2. 0.0.1 is already present and policy is IfNotPresent, so should not pull",
				name:            "wasm-test-module",
				tag:             "latest",
				policy:          "IfNotPresent",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.1",
			})

			// Intentionally, do not reset here to see the upgrade from 0.0.1.
			applyAndTestWasm(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2. 0.0.1 is already present. But policy is default and tag is latest, so pull the image",
				name:            "wasm-test-module",
				tag:             "latest",
				policy:          "",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.2",
			})
			resetWasm(t, "wasm-test-module")

			applyAndTestWasm(t, wasmTestConfigs{
				desc:            "initial creation with 0.0.1",
				name:            "wasm-test-module-test-tag-1",
				tag:             "test-tag-1",
				policy:          "",
				upstreamVersion: "0.0.1",
				expectedVersion: "0.0.1",
			})

			resetWasm(t, "wasm-test-module-test-tag-1")
			applyAndTestWasm(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2, but 0.0.1 is already present and policy is IfNotPresent",
				name:            "wasm-test-module-test-tag-1",
				tag:             "test-tag-1",
				policy:          "IfNotPresent",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.1",
			})

			resetWasm(t, "wasm-test-module-test-tag-1")
			applyAndTestWasm(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2, but 0.0.1 is already present and policy is default",
				name:            "wasm-test-module-test-tag-1",
				tag:             "test-tag-1",
				policy:          "",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.1",
			})

			// Intentionally, do not reset here to see the upgrade from 0.0.1.
			applyAndTestWasm(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2. 0.0.1 is already present but policy is Always, so pull 0.0.2",
				name:            "wasm-test-module-test-tag-1",
				tag:             "test-tag-1",
				policy:          "Always",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.2",
			})
		})
}

func installWasmExtension(ctx framework.TestContext, pluginName, tag, imagePullPolicy, pluginVersion string) error {
	wasmModuleURL := fmt.Sprintf("oci://%v/%v:%v", registry.Address(), imageName, tag)
	args := map[string]interface{}{
		"WasmPluginName":    pluginName,
		"TestWasmModuleURL": wasmModuleURL,
		"WasmPluginVersion": pluginVersion,
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

func sendTraffic() (echo.CallResult, error) {
	if len(common.GetClientInstances()) == 0 {
		return echo.CallResult{}, errors.New("there is no client")
	}
	cltInstance := common.GetClientInstances()[0]

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
			NoRetry: true,
		},
	}

	return cltInstance.Call(httpOpts)
}
