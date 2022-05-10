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

package wasmplugin

import (
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/components/registryredirector"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	common "istio.io/istio/tests/integration/telemetry/stats/prometheus"
)

var (
	client, server echo.Instances
	appNsInst      namespace.Instance
	promInst       prometheus.Instance
	registry       registryredirector.Instance
)

const (
	removedTag            = "source_principal"
	requestCountMultipler = 3
	httpProtocol          = "http"
	grpcProtocol          = "grpc"

	// Same user name and password as specified at pkg/test/fakes/imageregistry
	registryUser   = "user"
	registryPasswd = "passwd"

	imageName = "istio-testing/wasm/header-injector"

	injectedHeader = "x-resp-injection"
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
			return fmt.Errorf("[generation: %d] unexpected values for the header %q with the policy %q: given %q, got %q, want %q", generation, injectedHeader, c.policy, c.upstreamVersion, value, c.expectedVersion)
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

func TestWasmPluginPullPolicy(t *testing.T) {
	framework.NewTest(t).
		Features("extensibility.wasm.remote-load").
		Run(func(t framework.TestContext) {
			applyAndTestWasm(t, wasmTestConfigs{
				desc:            "initial creation with latest",
				name:            "wasm-test-module",
				tag:             "latest",
				policy:          "",
				upstreamVersion: "0.0.1",
				expectedVersion: "0.0.1"})

			resetWasm(t, "wasm-test-module")
			applyAndTestWasm(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2. 0.0.1 is already present and policy is IfNotPresent, so should not pull",
				name:            "wasm-test-module",
				tag:             "latest",
				policy:          "IfNotPresent",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.1"})

			// Intentionally, do not reset here to see the upgrade from 0.0.1.
			applyAndTestWasm(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2. 0.0.1 is already present. But policy is default and tag is latest, so pull the image",
				name:            "wasm-test-module",
				tag:             "latest",
				policy:          "",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.2"})
			resetWasm(t, "wasm-test-module")

			applyAndTestWasm(t, wasmTestConfigs{
				desc:            "initial creation with 0.0.1",
				name:            "wasm-test-module-test-tag-1",
				tag:             "test-tag-1",
				policy:          "",
				upstreamVersion: "0.0.1",
				expectedVersion: "0.0.1"})

			resetWasm(t, "wasm-test-module-test-tag-1")
			applyAndTestWasm(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2, but 0.0.1 is already present and policy is IfNotPresent",
				name:            "wasm-test-module-test-tag-1",
				tag:             "test-tag-1",
				policy:          "IfNotPresent",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.1"})

			resetWasm(t, "wasm-test-module-test-tag-1")
			applyAndTestWasm(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2, but 0.0.1 is already present and policy is default",
				name:            "wasm-test-module-test-tag-1",
				tag:             "test-tag-1",
				policy:          "",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.1"})

			// Intentionally, do not reset here to see the upgrade from 0.0.1.
			applyAndTestWasm(t, wasmTestConfigs{
				desc:            "upstream is upgraded to 0.0.2. 0.0.1 is already present but policy is Always, so pull 0.0.2",
				name:            "wasm-test-module-test-tag-1",
				tag:             "test-tag-1",
				policy:          "Always",
				upstreamVersion: "0.0.2",
				expectedVersion: "0.0.2"})

		})
}

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		Label(label.CustomSetup).
		Setup(istio.Setup(common.GetIstioInstance(), nil)).
		Setup(testSetup).
		Run()
}

func testSetup(ctx resource.Context) (err error) {
	registry, err = registryredirector.New(ctx, registryredirector.Config{
		Cluster: ctx.AllClusters().Default(),
	})
	if err != nil {
		return
	}

	var nsErr error
	appNsInst, nsErr = namespace.New(ctx, namespace.Config{
		Prefix: "echo",
		Inject: true,
	})
	if nsErr != nil {
		return nsErr
	}

	args := map[string]interface{}{
		"DockerConfigJson": base64.StdEncoding.EncodeToString(
			[]byte(createDockerCredential(registryUser, registryPasswd, registry.Address()))),
	}
	if err := ctx.ConfigIstio().EvalFile(appNsInst.Name(), args, "testdata/secret.yaml").
		Apply(); err != nil {
		return err
	}

	proxyMetadata := fmt.Sprintf(`
proxyMetadata:
  BOOTSTRAP_XDS_AGENT: "true"
  WASM_INSECURE_REGISTRIES: %q`, registry.Address())

	echos, err := deployment.New(ctx).
		WithClusters(ctx.Clusters()...).
		WithConfig(echo.Config{
			Service:   "client",
			Namespace: appNsInst,
			Ports:     nil,
			Subsets: []echo.SubsetConfig{
				{
					Annotations: map[echo.Annotation]*echo.AnnotationValue{
						echo.SidecarProxyConfig: {
							Value: proxyMetadata,
						},
					},
				},
			},
		}).
		WithConfig(echo.Config{
			Service:   "server",
			Namespace: appNsInst,
			Subsets: []echo.SubsetConfig{
				{
					Annotations: map[echo.Annotation]*echo.AnnotationValue{
						echo.SidecarProxyConfig: {
							Value: proxyMetadata,
						},
					},
				},
			},
			Ports: []echo.Port{
				{
					Name:         "http",
					Protocol:     protocol.HTTP,
					WorkloadPort: 8090,
				},
				{
					Name:         "grpc",
					Protocol:     protocol.GRPC,
					WorkloadPort: 7070,
				},
			},
		}).
		Build()
	if err != nil {
		return err
	}
	client = match.ServiceName(echo.NamespacedName{Name: "client", Namespace: appNsInst}).GetMatches(echos)
	server = match.ServiceName(echo.NamespacedName{Name: "server", Namespace: appNsInst}).GetMatches(echos)

	return nil
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

	if err := ctx.ConfigIstio().EvalFile(appNsInst.Name(), args, "testdata/wasmplugin.yaml").
		Apply(); err != nil {
		return err
	}

	return nil
}

func uninstallWasmExtension(ctx framework.TestContext, pluginName string) error {
	args := map[string]interface{}{
		"WasmPluginName": pluginName,
	}
	if err := ctx.ConfigIstio().EvalFile(appNsInst.Name(), args, "testdata/wasmplugin.yaml").Delete(); err != nil {
		return err
	}
	return nil
}

func sendTraffic() (echo.CallResult, error) {
	if len(client) == 0 {
		return echo.CallResult{}, errors.New("there is no client")
	}
	cltInstance := client[0]

	httpOpts := echo.CallOptions{
		To: server,
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

func createDockerCredential(user, passwd, registry string) string {
	credentials := `{
	"auths":{
		"%v":{
			"username": "%v",
			"password": "%v",
			"email": "test@abc.com",
			"auth": "%v"
		}
	}
}`
	auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + passwd))
	return fmt.Sprintf(credentials, registry, user, passwd, auth)
}
