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

package ingress

import (
	"fmt"
	"testing"
	"time"

	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	i    istio.Instance
	ingr ingress.Instance
)

// TestMain defines the entrypoint for pilot tests using a standard Istio installation.
// If a test requires a custom install it should go into its own package, otherwise it should go
// here to reuse a single install across tests.
func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.CustomSetup).

		// IngressClass is only present in 1.18+
		RequireEnvironmentVersion("1.18").
		RequireSingleCluster().
		Setup(func(ctx resource.Context) (err error) {
			if err := ctx.Config().ApplyYAMLDir("", "testdata"); err != nil {
				return err
			}
			return nil
		}).
		Setup(istio.Setup(&i, func(cfg *istio.Config) {
			cfg.Values["pilot.env.PILOT_ENABLED_SERVICE_APIS"] = "true"
		})).
		Setup(func(ctx resource.Context) (err error) {
			if ingr, err = ingress.New(ctx, ingress.Config{
				Istio: i,
			}); err != nil {
				return err
			}
			return nil
		}).
		Run()
}

func TestGateway(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "gateway",
				Inject: true,
			})
			var instance echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&instance, echo.Config{
					Service:   "server",
					Namespace: ns,
					Subsets:   []echo.SubsetConfig{{}},
					Ports: []echo.Port{
						{
							Name:     "http",
							Protocol: protocol.HTTP,
							// We use a port > 1024 to not require root
							InstancePort: 8090,
						},
					},
				}).
				BuildOrFail(t)
			instance.Address()
			if err := ctx.Config().ApplyYAML(ns.Name(), `
apiVersion: networking.x-k8s.io/v1alpha1
kind: GatewayClass
metadata:
  name: istio
spec:
  controller: istio.io/gateway-controller
---
apiVersion: networking.x-k8s.io/v1alpha1
kind: Gateway
metadata:
  name: gateway
spec:
  class: istio
  listeners:
  - hostname:
      match: Domain
      name: domain.example
    port: 80
    protocol: HTTP
    routes:
      namespaceSelector: {}
---
apiVersion: networking.x-k8s.io/v1alpha1
kind: HTTPRoute
metadata:
  name: http
spec:
  hosts:
  - hostnames: ["my.domain.example"]
    rules:
    - match:
        pathType: Prefix
        path: /get
      action:
        forwardTo:
          targetRef:
            name: server
            group: ""
            resource: ""`,
			); err != nil {
				t.Fatal(err)
			}

			if err := retry.UntilSuccess(func() error {
				resp, err := ingr.Call(ingress.CallOptions{
					Host:     "my.domain.example",
					Path:     "/get",
					CallType: ingress.PlainText,
					Address:  ingr.HTTPAddress(),
				})
				if err != nil {
					return err
				}
				if resp.Code != 200 {
					return fmt.Errorf("got invalid response code %v: %v", resp.Code, resp.Body)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
}

// TestIngress tests that we can route using standard Kubernetes Ingress objects.
func TestIngress(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "ingress",
				Inject: true,
			})
			var instance echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&instance, echo.Config{
					Service:   "server",
					Namespace: ns,
					Subsets:   []echo.SubsetConfig{{}},
					Ports: []echo.Port{
						{
							Name:     "http-test-port",
							Protocol: protocol.HTTP,
							// We use a port > 1024 to not require root
							InstancePort: 8090,
						},
					},
				}).
				BuildOrFail(t)
			instance.Address()

			// Set up secret contain some TLS certs for *.example.com
			// we will define one for foo.example.com and one for bar.example.com, to ensure both can co-exist
			credName := "k8s-ingress-secret-foo"
			ingressutil.CreateIngressKubeSecret(t, ctx, []string{credName}, ingress.TLS, ingressutil.IngressCredentialA, false)
			defer ingressutil.DeleteKubeSecret(t, ctx, []string{credName})
			credName2 := "k8s-ingress-secret-bar"
			ingressutil.CreateIngressKubeSecret(t, ctx, []string{credName2}, ingress.TLS, ingressutil.IngressCredentialB, false)
			defer ingressutil.DeleteKubeSecret(t, ctx, []string{credName2})

			if err := ctx.Config().ApplyYAML(ns.Name(), `
apiVersion: networking.k8s.io/v1beta1
kind: IngressClass
metadata:
  name: istio-test
spec:
  controller: istio.io/ingress-controller`, `
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  name: ingress
spec:
  ingressClassName: istio-test
  tls:
  - hosts: ["foo.example.com"]
    secretName: k8s-ingress-secret-foo
  - hosts: ["bar.example.com"]
    secretName: k8s-ingress-secret-bar
  rules:
    - http:
        paths:
          - path: /test/namedport
            backend:
              serviceName: server
              servicePort: http-test-port
          - path: /test
            backend:
              serviceName: server
              servicePort: 80`,
			); err != nil {
				t.Fatal(err)
			}

			cases := []struct {
				name string
				call ingress.CallOptions
			}{
				{
					// Basic HTTP call
					name: "http",
					call: ingress.CallOptions{
						Host:     "server",
						Path:     "/test",
						CallType: ingress.PlainText,
						Address:  ingr.HTTPAddress(),
					},
				},
				{
					// Basic HTTPS call for foo. CaCert matches the secret
					name: "https-foo",
					call: ingress.CallOptions{
						Host:     "foo.example.com",
						Path:     "/test",
						CallType: ingress.TLS,
						Address:  ingr.HTTPSAddress(),
						CaCert:   ingressutil.IngressCredentialA.CaCert,
					},
				},
				{
					// Basic HTTPS call for bar. CaCert matches the secret
					name: "https-bar",
					call: ingress.CallOptions{
						Host:     "bar.example.com",
						Path:     "/test",
						CallType: ingress.TLS,
						Address:  ingr.HTTPSAddress(),
						CaCert:   ingressutil.IngressCredentialB.CaCert,
					},
				},
				{
					// HTTPS call for bar with namedport route. CaCert matches the secret
					name: "https-namedport",
					call: ingress.CallOptions{
						Host:     "bar.example.com",
						Path:     "/test/namedport",
						CallType: ingress.TLS,
						Address:  ingr.HTTPSAddress(),
						CaCert:   ingressutil.IngressCredentialB.CaCert,
					},
				},
			}
			for _, tt := range cases {
				ctx.NewSubTest(tt.name).Run(func(t framework.TestContext) {
					retry.UntilSuccessOrFail(t, func() error {
						resp, err := ingr.Call(tt.call)
						if err != nil {
							return err
						}
						if resp.Code != 200 {
							return fmt.Errorf("got invalid response code %v: %v", resp.Code, resp.Body)
						}
						return nil
					}, retry.Delay(time.Millisecond*100), retry.Timeout(time.Minute*2))
				})
			}
		})
}
