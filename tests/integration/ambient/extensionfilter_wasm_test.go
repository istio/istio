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
	efWasmImageName      = "istio-testing/wasm/header-injector"
	efWasmInjectedHeader = "x-resp-injection"
	efWasmConfigFile     = "testdata/extensionfilter-wasm.yaml"
)

type extensionFilterWasmTestConfig struct {
	desc         string
	name         string
	testHostname string
}

var efWasmGeneration = 0

func applyAndTestExtensionFilterWasmWithOCI(ctx framework.TestContext, c extensionFilterWasmTestConfig, path, targetType, targetName string) {
	ctx.NewSubTest("OCI_" + c.desc).Run(func(t framework.TestContext) {
		defer func() {
			efWasmGeneration++
		}()
		mapTagToVersionOrFail(t, "latest", "0.0.1")
		wasmModuleURL := fmt.Sprintf("oci://%v/%v:%v", registry.Address(), efWasmImageName, "latest")
		if err := installExtensionFilterWasm(t, c.name, wasmModuleURL, "", fmt.Sprintf("g-%d", efWasmGeneration), targetType, targetName, path); err != nil {
			t.Fatalf("failed to install ExtensionFilter: %v", err)
		}
		if c.testHostname != "" {
			sendTrafficToHostname(t, check.ResponseHeader(efWasmInjectedHeader, "0.0.1"), c.testHostname)
		} else {
			sendTraffic(t, check.ResponseHeader(efWasmInjectedHeader, "0.0.1"))
		}
	})
}

func resetExtensionFilterWasmConfig(ctx framework.TestContext, filterName, path string) {
	ctx.NewSubTest("Delete ExtensionFilter " + filterName).Run(func(t framework.TestContext) {
		if err := uninstallExtensionFilterWasm(t, filterName, path); err != nil {
			t.Fatal(err)
		}
		sendTraffic(t, check.ResponseHeader(efWasmInjectedHeader, ""), retry.Converge(2))
	})
}

func applyExtensionFilterWasmConfigFile(ctx framework.TestContext, ns string, args map[string]any, path string) error {
	return ctx.ConfigIstio().EvalFile(ns, args, path).Apply()
}

func installExtensionFilterWasm(ctx framework.TestContext, filterName, wasmModuleURL, imagePullPolicy, filterVersion, targetType, targetName, path string) error {
	kind, group, name := getTargetRefValues(targetType, targetName)

	args := map[string]any{
		"ExtensionFilterName": filterName,
		"TestWasmModuleURL":   wasmModuleURL,
		"FilterVersion":       filterVersion,
		"TargetKind":          kind,
		"TargetGroup":         group,
		"TargetName":          name,
	}

	if len(imagePullPolicy) != 0 {
		args["ImagePullPolicy"] = imagePullPolicy
	}

	if err := applyExtensionFilterWasmConfigFile(ctx, apps.Namespace.Name(), args, path); err != nil {
		return err
	}

	return nil
}

func uninstallExtensionFilterWasm(ctx framework.TestContext, filterName, path string) error {
	args := map[string]any{
		"ExtensionFilterName": filterName,
	}
	if err := ctx.ConfigIstio().EvalFile(apps.Namespace.Name(), args, path).Delete(); err != nil {
		return err
	}
	return nil
}

// TestExtensionFilter_WasmConfigurations tests WASM ExtensionFilter on different targets in ambient mode
func TestExtensionFilter_WasmConfigurations(t *testing.T) {
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
					desc:         "Configure WASM filter for gateway",
					name:         "gateway-wasm-extensionfilter",
					testHostname: fmt.Sprintf("%s-gateway-istio.%s.svc.cluster.local", GetTarget().ServiceName(), apps.Namespace.Name()),
					targetType:   "gateway",
					targetName:   fmt.Sprintf("%s-gateway", GetTarget().(echo.Instances).ServiceName()),
				},
				{
					desc:       "Configure WASM filter for waypoint",
					name:       "waypoint-wasm-extensionfilter",
					targetType: "gateway",
					targetName: constants.DefaultNamespaceWaypoint,
				},
				{
					desc:       "Configure WASM filter for specific service",
					name:       "service-wasm-extensionfilter",
					targetType: "service",
					targetName: GetTarget().Instances().ServiceName(),
				},
			}

			for _, tc := range testCases {
				if tc.name == "gateway-wasm-extensionfilter" {
					crd.DeployGatewayAPIOrSkip(t)
					args := map[string]any{
						"To": GetTarget().Instances(),
					}
					t.ConfigIstio().EvalFile(apps.Namespace.Name(), args, "testdata/gateway-api.yaml").ApplyOrFail(t)
				}

				applyAndTestExtensionFilterWasmWithOCI(t, extensionFilterWasmTestConfig{
					desc:         tc.desc,
					name:         tc.name,
					testHostname: tc.testHostname,
				}, efWasmConfigFile, tc.targetType, tc.targetName)

				resetExtensionFilterWasmConfig(t, tc.name, efWasmConfigFile)
			}
		})
}
