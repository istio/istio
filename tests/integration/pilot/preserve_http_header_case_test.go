//go:build integ
// +build integ

// Copyright Istio Authors
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

package pilot

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"istio.io/api/annotation"
	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

func TestPreserveHTTPHeaderCaseConfiguration(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(ctx, namespace.Config{
				Prefix: "echo-test",
				Inject: true,
			})

			echos := deployment.New(ctx)
			echos.WithClusters(ctx.Clusters()...)
			echos.WithConfig(echo.Config{
				Service:   "client",
				Namespace: ns,
				Ports:     ports.All(),
			})
			echos.WithConfig(echo.Config{
				Service:   "server",
				Namespace: ns,
				Ports:     ports.All(),
				Subsets: []echo.SubsetConfig{
					{
						Annotations: map[string]string{
							annotation.ProxyConfig.Name: `
proxyHeaders:
  preserveHttp1HeaderCase: true`,
						},
					},
				},
			})
			workloads := echos.BuildOrFail(t)
			server := match.ServiceName(echo.NamespacedName{Name: "server", Namespace: ns}).GetMatches(workloads)

			// Verify Cluster Configuration
			serverPodName := server[0].WorkloadsOrFail(ctx)[0].PodName()
			output, _ := istioctl.NewOrFail(ctx, istioctl.Config{}).InvokeOrFail(ctx,
				[]string{"proxy-config", "cluster", serverPodName, "--namespace", ns.Name(), "-o", "json"})
			assert.Contains(t, output, `"name": "preserve_case"`, "preserve_case configuration not found in cluster")
			assert.Contains(t, output,
				`"@type": "type.googleapis.com/envoy.extensions.http.header_formatters.preserve_case.v3.PreserveCaseFormatterConfig"`,
				"preserve_case type configuration not found in cluster")

			// Verify Passthrough Cluster
			clusters := []map[string]json.RawMessage{}
			assert.NoError(t, json.Unmarshal([]byte(output), &clusters), "failed to unmarshal clusters")
			for _, c := range clusters {
				if string(c["name"]) == "\"PassthroughCluster\"" {
					assert.Contains(t, string(c["typedExtensionProtocolOptions"]),
						`"@type": "type.googleapis.com/envoy.extensions.http.header_formatters.preserve_case.v3.PreserveCaseFormatterConfig"`,
						"preserve_case type configuration not found in passthrough cluster")
					break
				}
			}

			// Verify Listener Configuration
			output, _ = istioctl.NewOrFail(ctx, istioctl.Config{}).InvokeOrFail(ctx,
				[]string{"proxy-config", "listener", serverPodName, "--namespace", ns.Name(), "-o", "json"})
			assert.Contains(t, output, `"name": "preserve_case"`, "preserve_case configuration not found in listener")
			assert.Contains(t, output,
				`"@type": "type.googleapis.com/envoy.extensions.http.header_formatters.preserve_case.v3.PreserveCaseFormatterConfig"`,
				"preserve_case type configuration not found in listener")
		})
}

var (
	customHeaderKey   = "X-Custom-Header"
	customHeaderValue = "CustomValue"
)

func TestPreserveHTTPHeaderCase(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(ctx, namespace.Config{
				Prefix: "echo-test",
				Inject: true,
			})

			echos := deployment.New(ctx)
			echos.WithClusters(ctx.Clusters()...)
			echos.WithConfig(echo.Config{
				Service:   "client",
				Namespace: ns,
				Ports:     ports.All(),
			})
			echos.WithConfig(echo.Config{
				Service:   "server",
				Namespace: ns,
				Ports:     ports.All(),
				Subsets: []echo.SubsetConfig{
					{
						Annotations: map[string]string{
							annotation.ProxyConfig.Name: `
                proxyHeaders:
                  preserveHttp1HeaderCase: true`,
						},
					},
				},
			})
			workloads := echos.BuildOrFail(t)
			client := match.ServiceName(echo.NamespacedName{Name: "client", Namespace: ns}).GetMatches(workloads)
			server := match.ServiceName(echo.NamespacedName{Name: "server", Namespace: ns}).GetMatches(workloads)

			// Send HTTP/1.x Traffic and Validate Headers
			client[0].CallOrFail(ctx, echo.CallOptions{
				To:   server[0],
				Port: ports.HTTP,
				HTTP: echo.HTTP{
					Path: "/test",
					Headers: map[string][]string{
						customHeaderKey: {customHeaderValue},
					},
				},
				Check: check.And(
					check.OK(),
					check.Each(func(response echoClient.Response) error {
						actualValues, ok := response.RequestHeaders[customHeaderKey]
						if !ok || len(actualValues) == 0 || actualValues[0] != customHeaderValue {
							return fmt.Errorf("expected header '%s' with value '%s', but got: %v",
								customHeaderKey, customHeaderValue, response.RequestHeaders)
						}
						return nil
					}),
				),
			})
		})
}
