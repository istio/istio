//go:build integ

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

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/util/retry"
)

// TestHTTPRouteAndGRPCRouteCoexistence tests that HTTPRoute and GRPCRoute can coexist on the same gateway hostname
func TestHTTPRouteAndGRPCRouteCoexistence(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			t.ConfigIstio().YAML(apps.Namespace.Name(), `
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: gateway
spec:
  gatewayClassName: istio
  listeners:
  - name: default
    hostname: "*.example.com"
    port: 80
    protocol: HTTP
    allowedRoutes:
      namespaces:
        from: All
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: http
spec:
  parentRefs:
  - name: gateway
  hostnames: ["my.example.com"]
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: /http
    backendRefs:
    - name: b
      port: 80
---
apiVersion: gateway.networking.k8s.io/v1
kind: GRPCRoute
metadata:
  name: grpc
spec:
  parentRefs:
  - name: gateway
  hostnames: ["my.example.com"]
  rules:
  - backendRefs:
    - name: b
      port: 7070
`).ApplyOrFail(t)

			gatewayAddr := fmt.Sprintf("gateway-istio.%s.svc.cluster.local", apps.Namespace.Name())

			// Test HTTP traffic
			t.NewSubTest("http-traffic").Run(func(t framework.TestContext) {
				retry.UntilSuccessOrFail(t, func() error {
					_, err := apps.A[0].Call(echo.CallOptions{
						Port: echo.Port{
							Protocol:    protocol.HTTP,
							ServicePort: 80,
						},
						Scheme: scheme.HTTP,
						HTTP: echo.HTTP{
							Path:    "/http",
							Headers: headers.New().WithHost("my.example.com").Build(),
						},
						Address: gatewayAddr,
						Check:   check.OK(),
					})
					return err
				})
			})

			// Test gRPC traffic
			t.NewSubTest("grpc-traffic").Run(func(t framework.TestContext) {
				retry.UntilSuccessOrFail(t, func() error {
					_, err := apps.A[0].Call(echo.CallOptions{
						Port: echo.Port{
							Protocol:    protocol.GRPC,
							ServicePort: 80,
						},
						Scheme: scheme.GRPC,
						HTTP: echo.HTTP{
							Headers: headers.New().WithHost("my.example.com").Build(),
						},
						Address: gatewayAddr,
						Check:   check.OK(),
					})
					return err
				})
			})
		})
}
