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

package ambient

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/crd"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	imageName      = "istio-testing/wasm/header-injector"
	injectedHeader = "x-resp-injection"
	wasmConfigFile = "testdata/wasm-filter.yaml"
)

type wasmTestConfigs struct {
	desc         string
	name         string
	testHostname string
}

var generation = 0

func GetClientInstances() echo.Instances {
	return apps.Captured
}

func GetTarget() echo.Target {
	return apps.ServiceAddressedWaypoint
}

func mapTagToVersionOrFail(t framework.TestContext, tag, version string) {
	t.Helper()
	if err := registry.SetupTagMap(map[string]string{
		imageName + ":" + tag: version,
	}); err != nil {
		t.Fatalf("failed to setup the tag map: %v", err)
	}
}

func applyAndTestCustomWasmConfigWithOCI(ctx framework.TestContext, c wasmTestConfigs, path, targetType, targetName string) {
	ctx.NewSubTest("OCI_" + c.desc).Run(func(t framework.TestContext) {
		defer func() {
			generation++
		}()
		mapTagToVersionOrFail(t, "latest", "0.0.1")
		wasmModuleURL := fmt.Sprintf("oci://%v/%v:%v", registry.Address(), imageName, "latest")
		if err := installWasmExtension(t, c.name, wasmModuleURL, "", fmt.Sprintf("g-%d", generation), targetType, targetName, path); err != nil {
			t.Fatalf("failed to install WasmPlugin: %v", err)
		}
		if c.testHostname != "" {
			sendTrafficToHostname(t, check.ResponseHeader(injectedHeader, "0.0.1"), c.testHostname)
		} else {
			sendTraffic(t, check.ResponseHeader(injectedHeader, "0.0.1"))
		}
	})
}

func resetCustomWasmConfig(ctx framework.TestContext, pluginName, path string) {
	ctx.NewSubTest("Delete WasmPlugin " + pluginName).Run(func(t framework.TestContext) {
		if err := uninstallWasmExtension(t, pluginName, path); err != nil {
			t.Fatal(err)
		}
		sendTraffic(t, check.ResponseHeader(injectedHeader, ""), retry.Converge(2))
	})
}

func applyWasmConfig(ctx framework.TestContext, ns string, args map[string]any, path string) error {
	return ctx.ConfigIstio().EvalFile(ns, args, path).Apply()
}

func installWasmExtension(ctx framework.TestContext, pluginName, wasmModuleURL, imagePullPolicy, pluginVersion, targetType, targetName, path string) error {
	kind, group, name := getTargetRefValues(targetType, targetName)

	args := map[string]any{
		"WasmPluginName":    pluginName,
		"TestWasmModuleURL": wasmModuleURL,
		"WasmPluginVersion": pluginVersion,
		"TargetKind":        kind,
		"TargetGroup":       group,
		"TargetName":        name,
	}

	if len(imagePullPolicy) != 0 {
		args["ImagePullPolicy"] = imagePullPolicy
	}

	if err := applyWasmConfig(ctx, apps.Namespace.Name(), args, path); err != nil {
		return err
	}

	return nil
}

func uninstallWasmExtension(ctx framework.TestContext, pluginName, path string) error {
	args := map[string]any{
		"WasmPluginName": pluginName,
	}
	if err := ctx.ConfigIstio().EvalFile(apps.Namespace.Name(), args, path).Delete(); err != nil {
		return err
	}
	return nil
}

func sendTraffic(ctx framework.TestContext, checker echo.Checker, options ...retry.Option) {
	ctx.Helper()
	if len(GetClientInstances()) == 0 {
		ctx.Fatal("there is no client")
	}
	// TODO: Figure out if/how to support multiple clusters
	cltInstance := GetClientInstances().ForCluster(ctx.Clusters().Default().Name())[0]

	defaultOptions := []retry.Option{retry.Delay(100 * time.Millisecond), retry.Timeout(200 * time.Second)}
	httpOpts := echo.CallOptions{
		To: GetTarget().Instances().ForCluster(ctx.Clusters().Default().Name()),
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

func sendTrafficToHostname(ctx framework.TestContext, checker echo.Checker, hostname string, options ...retry.Option) {
	ctx.Helper()
	if len(GetClientInstances()) == 0 {
		ctx.Fatal("there is no client")
	}
	cltInstance := GetClientInstances()[0]

	defaultOptions := []retry.Option{retry.Delay(100 * time.Millisecond), retry.Timeout(200 * time.Second)}
	httpOpts := echo.CallOptions{
		Address: hostname,
		Port: echo.Port{
			Name:        "http",
			ServicePort: 80,
			Protocol:    protocol.HTTP,
		},
		HTTP: echo.HTTP{
			Path:    "/path",
			Method:  "GET",
			Headers: headers.New().WithHost(fmt.Sprintf("%s.com", GetTarget().ServiceName())).Build(),
		},
		Count: 1,
		Retry: echo.Retry{
			Options: append(defaultOptions, options...),
		},
		Check: checker,
	}

	_ = cltInstance.CallOrFail(ctx, httpOpts)
}

func getTargetRefValues(targetType, targetName string) (kind, group, name string) {
	switch targetType {
	case "gateway":
		return "Gateway", "gateway.networking.k8s.io", targetName
	case "service":
		return "Service", "\"\"", targetName
	default:
		return "", "", ""
	}
}

func TestWasmPluginConfigurations(t *testing.T) {
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
					desc:         "Configure WebAssembly filter for gateway",
					name:         "gateway-wasm-test",
					testHostname: fmt.Sprintf("%s-gateway-istio.%s.svc.cluster.local", GetTarget().ServiceName(), apps.Namespace.Name()),
					targetType:   "gateway",
					targetName:   fmt.Sprintf("%s-gateway", GetTarget().(echo.Instances).ServiceName()),
				},
				{
					desc:       "Configure WebAssembly filter for waypoint",
					name:       "waypoint-wasm-test",
					targetType: "gateway",
					targetName: constants.DefaultNamespaceWaypoint,
				},
				{
					desc:       "Configure WebAssembly filter for specific service",
					name:       "service-wasm-test",
					targetType: "service",
					targetName: GetTarget().Instances().ServiceName(),
				},
			}

			for _, tc := range testCases {
				if tc.name == "gateway-wasm-test" {
					crd.DeployGatewayAPIOrSkip(t)
					args := map[string]any{
						"To": GetTarget().Instances(),
					}
					t.ConfigIstio().EvalFile(apps.Namespace.Name(), args, "testdata/gateway-api.yaml").ApplyOrFail(t)
				}

				applyAndTestCustomWasmConfigWithOCI(t, wasmTestConfigs{
					desc:         tc.desc,
					name:         tc.name,
					testHostname: tc.testHostname,
				}, wasmConfigFile, tc.targetType, tc.targetName)

				resetCustomWasmConfig(t, tc.name, wasmConfigFile)
			}
		})
}
