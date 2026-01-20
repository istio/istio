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

package resourcefilter

import (
	"fmt"
	"path/filepath"
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/crd"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/util/file"
)

// TestPilotResourceFilter verifies that Pilot was started correctly filtering resources
// to be reconciled, and being able to reconcile Gateway API resources and
// WasmPlugins as it is explicitly included as a valid resource, but ignoring
// any other Istio resource.
func TestPilotResourceFilter(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			crd.DeployGatewayAPIOrSkip(t)
			t.NewSubTest("gatewayonly").Run(GatewayTest)
		})
}

func GatewayTest(t framework.TestContext) {
	t.ConfigIstio().YAML(apps.Namespace.Name(), `apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: gateway
spec:
  gatewayClassName: istio
  allowedListeners:
    namespaces:
      from: All
  listeners:
  - name: default
    hostname: "*.example.com"
    port: 80
    protocol: HTTP
---
`).ApplyOrFail(t)

	// BackendTLSPolicy is explicitly ignored and should fail. In case of a failure
	// to establish a connection to a TLS backend, the gateway returns code 400
	t.NewSubTest("backend-tls-should-pass").Run(func(t framework.TestContext) {
		ca := file.AsStringOrFail(t, filepath.Join(env.IstioSrc, "tests/testdata/certs/dns/root-cert.pem"))
		t.ConfigIstio().Eval(apps.Namespace.Name(), ca, `
apiVersion: v1
kind: ConfigMap
data:
  ca.crt: |
{{. | indent 4}}
metadata:
  name: auth-cert
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: tls
spec:
  parentRefs:
  - name: gateway
  hostnames: ["btlspolicy.example.com"]
  rules:
  - backendRefs:
    - name: b
      port: 443
---
apiVersion: gateway.networking.k8s.io/v1
kind: BackendTLSPolicy
metadata:
  name: tls-upstream
spec:
  targetRefs:
  - group: ""
    kind: Service
    name: b
  validation:
    caCertificateRefs:
    - group: ""
      kind: ConfigMap
      name: auth-cert
    hostname: auth.example.com
`).ApplyOrFail(t)
		apps.A[0].CallOrFail(t, echo.CallOptions{
			Port: echo.Port{
				Protocol:    protocol.HTTP,
				ServicePort: 80,
			},
			Scheme: scheme.HTTP,
			HTTP: echo.HTTP{
				Headers: headers.New().WithHost("btlspolicy.example.com").Build(),
			},
			Address: fmt.Sprintf("gateway-istio.%s.svc.cluster.local", apps.Namespace.Name()),
			Check:   check.And(check.Status(200)),
		})
	})

	// Using envoyfilter should not work, so even trying to force breaking the
	// configuration will have no result
	t.NewSubTest("disallow-envoy-filter").Run(func(t framework.TestContext) {
		t.ConfigIstio().Eval(apps.Namespace.Name(), nil, `
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: changeheader
spec:
  parentRefs:
  - name: gateway
  hostnames: ["changeheader.example.com"]
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: /path
    filters:
    - type: RequestHeaderModifier
      requestHeaderModifier:
        add:
        - name: my-added-header
          value: added-value
    backendRefs:
    - name: b
      port: 80
---
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: change-status
spec:
  targetRefs:
  - kind: Gateway
    name: gateway
    group: gateway.networking.k8s.io
  configPatches:
  - applyTo: HTTP_FILTER
    match:
      context: GATEWAY
      listener:
        filterChain:
          filter:
            name: "envoy.filters.network.http_connection_manager"
    patch:
      operation: INSERT_BEFORE
      value:
        name: envoy.filters.http.local_ratelimit
        typed_config:
          # Filter below is missing configurations and should cause a 503, but EnvoyFilter is disabled
          "@type": type.googleapis.com/envoy.extensions.filters.http.local_ratelimit.v3.LocalRateLimit
          status:
            code: 429
`).ApplyOrFail(t)
		// Calling the APP should work. The envoy filter is not applied because it is
		// ignored.
		// Adding it to the inclusion list on filter would rather cause the below test to
		// receive a 503
		apps.A[0].CallOrFail(t, echo.CallOptions{
			Port: echo.Port{
				Protocol:    protocol.HTTP,
				ServicePort: 80,
			},
			HTTP: echo.HTTP{
				Headers: headers.New().WithHost("changeheader.example.com").Build(),
				Path:    "/path",
			},
			Address: fmt.Sprintf("gateway-istio.%s.svc.cluster.local", apps.Namespace.Name()),
			Check: check.And(
				check.OK(),
				check.RequestHeader("My-Added-Header", "added-value")),
		})
	})

	// This test will create an HTTPRoute, and also attach an invalid WasmPlugin
	// to the route.
	// When WASMPlugin reconciliation is enabled and it is invalid,
	// it should return an error instead  of HTTP/OK.
	t.NewSubTest("allow-using-wasm-filter").Run(func(t framework.TestContext) {
		t.ConfigIstio().Eval(apps.Namespace.Name(), nil, `
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: tls
spec:
  parentRefs:
  - name: gateway
  hostnames: ["wasmplugin.example.com"]
  rules:
  - backendRefs:
    - name: b
      port: 80
---
apiVersion: extensions.istio.io/v1alpha1
kind: WasmPlugin
metadata:
  name: gateway-wasm-test
spec:
  phase: STATS
  targetRef:
    kind: Gateway
    group: gateway.networking.k8s.io
    name: gateway
  url: oci://127.0.0.1/istio-testing/wasm/invalid-wasm:latest
  failStrategy: FAIL_CLOSE
`).ApplyOrFail(t)

		// Calling the APP should work and get a 403 error.
		// The WASMPlugin is invalid, and there's a failclose, so it will return
		// an error 403
		apps.A[0].CallOrFail(t, echo.CallOptions{
			Port: echo.Port{
				Protocol:    protocol.HTTP,
				ServicePort: 80,
			},
			HTTP: echo.HTTP{
				Headers: headers.New().WithHost("wasmplugin.example.com").Build(),
				Path:    "/",
			},
			Address: fmt.Sprintf("gateway-istio.%s.svc.cluster.local", apps.Namespace.Name()),
			Check:   check.Status(403), // WASMPlugin with error means a 403 here
		})
	})
}
