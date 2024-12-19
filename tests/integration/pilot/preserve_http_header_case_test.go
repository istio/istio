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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
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

var (
	customHeaders = []map[string][]string{
		{"X-Custom-Header": {"CustomValue"}},
		{"x-Custom-Header": {"CustomValue"}},
		{"X-custom-header": {"CustomValue"}},
		{"x-custom-header": {"CustomValue"}},
		{"X-CUSTOM-HEADER": {"CustomValue"}},
		{"x-CUSTOM-HEADER": {"CustomValue"}},
	}
)

func TestPreserveHTTPHeaderCase(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(ctx, namespace.Config{
				Prefix: "echo-test",
				Inject: true,
			})

			// Deploy Echo Workloads
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
			})
			workloads := echos.BuildOrFail(t)
			client := match.ServiceName(echo.NamespacedName{Name: "client", Namespace: ns}).GetMatches(workloads)
			server := match.ServiceName(echo.NamespacedName{Name: "server", Namespace: ns}).GetMatches(workloads)

			meshConfig := `
values:
meshConfig:
  defaultConfig:
    proxyHeaders:
      preserveHttp1HeaderCase: true
`
			ctx.ConfigIstio().YAML("istio-system", meshConfig).ApplyOrFail(ctx)

			// Verify Cluster Configuration
			serverPodName := server[0].WorkloadsOrFail(ctx)[0].PodName()
			output, _ := istioctl.NewOrFail(ctx, istioctl.Config{}).InvokeOrFail(ctx, []string{"proxy-config", "cluster", serverPodName, "--namespace", ns.Name()})
			assert.Contains(t, output, `"name": "preserve_case"`, "preserve_case configuration not found")
			// assert.Contains(t, output, `"type_url": "type.googleapis.com/envoy.extensions.http.header_formatters.preserve_case.v3.PreserveCaseFormatterConfig"`, "preserve_case type configuration not found")

			// Send HTTP/1.x Traffic and Validate Headers
			for _, headers := range customHeaders {

				for key, values := range headers {
					client[0].CallOrFail(ctx, echo.CallOptions{
						To:   server[0],
						Port: ports.HTTP,
						HTTP: echo.HTTP{
							Path:    "/test",
							Headers: headers,
						},
						Check: check.And(
							check.OK(),
							check.Each(func(response echoClient.Response) error {
								val, ok := response.RequestHeaders[key]
								assert.True(t, ok && val[0] == values[0],
									fmt.Sprintf("expected header '%s' with value '%s', but got: %v", key, values[0], response.RequestHeaders))
								return nil
							}),
						),
					})
				}
			}

			// Verify Listener Configuration
		})
}
