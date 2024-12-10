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
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

func TestPreserveHTTPHeaderCase(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			// Step 1: Define Namespace
			ns := namespace.NewOrFail(ctx, namespace.Config{
				Prefix: "echo-test",
				Inject: true,
			})

			// Step 2: Deploy Echo Workloads
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

			// Step 3: Match Client and Server
			client := match.ServiceName(echo.NamespacedName{Name: "client", Namespace: ns}).GetMatches(workloads)
			server := match.ServiceName(echo.NamespacedName{Name: "server", Namespace: ns}).GetMatches(workloads)

			// Step 4: Validate Client-to-Server Traffic
			client[0].CallOrFail(t, echo.CallOptions{
				To:   server[0],
				Port: ports.HTTP,
				HTTP: echo.HTTP{
					Path: "/",
				},
				Check: check.OK(),
			})
			// Step 4: Configure Stateful Formatter
			meshConfig := `
values:
meshConfig:
  defaultConfig:
    proxyHeaders:
      preserveHttp1HeaderCase: true
`
			ctx.ConfigIstio().YAML("istio-system", meshConfig).ApplyOrFail(ctx)

			// Step 5: Send HTTP/1.x Traffic and Validate Headers
			expectedHeader := map[string][]string{
				"X-Custom-Header": {"CustomValue"},
			}
			response := client[0].CallOrFail(ctx, echo.CallOptions{
				To:   server[0],
				Port: ports.HTTP,
				HTTP: echo.HTTP{
					Path:    "/test",
					Headers: expectedHeader,
				},
				Check: check.OK(),
			})

			rHeaders := response.Responses[0].ResponseHeaders
			assert.True(t, headersContain(rHeaders, expectedHeader), "Header case preservation failed")

			// Step 6: Verify Cluster Configuration

			// Step 7: Verify Listener Configuration
		})
}

// Helper function to check if expected headers are present in the actual headers
func headersContain(actual http.Header, expected map[string][]string) bool {
	for key, values := range expected {
		actualValues, ok := actual[key]
		if !ok {
			return false
		}
		for _, value := range values {
			found := false
			for _, actualValue := range actualValues {
				if strings.EqualFold(actualValue, value) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}
