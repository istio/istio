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
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/util/retry"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

func TestGateway(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			if !supportsCRDv1(ctx) {
				t.Skip("Not supported; requires CRDv1 support.")
			}
			ctx.Config().ApplyYAMLOrFail(ctx, apps.Namespace.Name(), `
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
  gatewayClassName: istio
  listeners:
  - hostname: "*.domain.example"
    port: 80
    protocol: HTTP
    routes:
      kind: HTTPRoute
  - port: 31400
    protocol: TCP
    routes:
      kind: TCPRoute
---
apiVersion: networking.x-k8s.io/v1alpha1
kind: HTTPRoute
metadata:
  name: http
spec:
 hostnames: ["my.domain.example"]
 rules:
 - matches:
   - path:
       type: Prefix
       value: /get
   forwardTo:
     - serviceName: b
       port: 80
---
apiVersion: networking.x-k8s.io/v1alpha1
kind: TCPRoute
metadata:
  name: tcp
spec:
  rules:
  - forwardTo:
     - serviceName: b
       port: 80
`)

			ctx.NewSubTest("http").Run(func(ctx framework.TestContext) {
				_ = apps.Ingress.CallEchoWithRetryOrFail(ctx, echo.CallOptions{
					Port: &echo.Port{
						Protocol: protocol.HTTP,
					},
					Path: "/get",
					Headers: map[string][]string{
						"Host": {"my.domain.example"},
					},
					Validator: echo.ExpectOK(),
				})
			})
			ctx.NewSubTest("tcp").Run(func(ctx framework.TestContext) {
				address := apps.Ingress.TCPAddress()
				_ = apps.Ingress.CallEchoWithRetryOrFail(ctx, echo.CallOptions{
					Port: &echo.Port{
						Protocol:    protocol.HTTP,
						ServicePort: address.Port,
					},
					Address: address.IP.String(),
					Path:    "/",
					Headers: map[string][]string{
						"Host": {"my.domain.example"},
					},
					Validator: echo.ExpectOK(),
				})
			})
		})
}

func skipIfIngressClassUnsupported(ctx framework.TestContext) {
	if !ctx.Clusters().Default().MinKubeVersion(1, 18) {
		ctx.Skip("IngressClass not supported")
	}
}

// TestIngress tests that we can route using standard Kubernetes Ingress objects.
func TestIngress(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			if ctx.Clusters().IsMulticluster() {
				t.Skip("TODO convert this test to support multicluster")
			}
			skipIfIngressClassUnsupported(ctx)
			// Set up secret contain some TLS certs for *.example.com
			// we will define one for foo.example.com and one for bar.example.com, to ensure both can co-exist
			credName := "k8s-ingress-secret-foo"
			ingressutil.CreateIngressKubeSecret(ctx, []string{credName}, ingressutil.TLS, ingressutil.IngressCredentialA, false)
			ctx.ConditionalCleanup(func() {
				ingressutil.DeleteKubeSecret(ctx, []string{credName})
			})
			credName2 := "k8s-ingress-secret-bar"
			ingressutil.CreateIngressKubeSecret(ctx, []string{credName2}, ingressutil.TLS, ingressutil.IngressCredentialB, false)
			ctx.ConditionalCleanup(func() {
				ingressutil.DeleteKubeSecret(ctx, []string{credName2})
			})

			ingressClassConfig := `
apiVersion: networking.k8s.io/v1beta1
kind: IngressClass
metadata:
  name: istio-test
spec:
  controller: istio.io/ingress-controller`

			ingressConfigTemplate := `
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  name: %s
spec:
  ingressClassName: %s
  tls:
  - hosts: ["foo.example.com"]
    secretName: k8s-ingress-secret-foo
  - hosts: ["bar.example.com"]
    secretName: k8s-ingress-secret-bar
  rules:
    - http:
        paths:
          - path: %s/namedport
            backend:
              serviceName: b
              servicePort: http
          - path: %s
            backend:
              serviceName: b
              servicePort: 80`

			if err := ctx.Config().ApplyYAML(apps.Namespace.Name(), ingressClassConfig,
				fmt.Sprintf(ingressConfigTemplate, "ingress", "istio-test", "/test", "/test")); err != nil {
				t.Fatal(err)
			}

			// TODO check all clusters were hit
			cases := []struct {
				name string
				call echo.CallOptions
			}{
				{
					// Basic HTTP call
					name: "http",
					call: echo.CallOptions{
						Port: &echo.Port{
							Protocol: protocol.HTTP,
						},
						Path: "/test",
						Headers: map[string][]string{
							"Host": {"server"},
						},
						Validator: echo.ExpectOK(),
					},
				},
				{
					// Basic HTTPS call for foo. CaCert matches the secret
					name: "https-foo",
					call: echo.CallOptions{
						Port: &echo.Port{
							Protocol: protocol.HTTPS,
						},
						Path: "/test",
						Headers: map[string][]string{
							"Host": {"foo.example.com"},
						},
						CaCert:    ingressutil.IngressCredentialA.CaCert,
						Validator: echo.ExpectOK(),
					},
				},
				{
					// Basic HTTPS call for bar. CaCert matches the secret
					name: "https-bar",
					call: echo.CallOptions{
						Port: &echo.Port{
							Protocol: protocol.HTTPS,
						},
						Path: "/test",
						Headers: map[string][]string{
							"Host": {"bar.example.com"},
						},
						CaCert:    ingressutil.IngressCredentialB.CaCert,
						Validator: echo.ExpectOK(),
					},
				},
				{
					// HTTPS call for bar with namedport route. CaCert matches the secret
					name: "https-namedport",
					call: echo.CallOptions{
						Port: &echo.Port{
							Protocol: protocol.HTTPS,
						},
						Path: "/test/namedport",
						Headers: map[string][]string{
							"Host": {"bar.example.com"},
						},
						CaCert:    ingressutil.IngressCredentialB.CaCert,
						Validator: echo.ExpectOK(),
					},
				},
			}
			for _, c := range cases {
				c := c
				ctx.NewSubTest(c.name).Run(func(ctx framework.TestContext) {
					apps.Ingress.CallEchoWithRetryOrFail(ctx, c.call, retry.Timeout(time.Minute*2))
				})
			}

			ctx.NewSubTest("status").Run(func(ctx framework.TestContext) {
				if !ctx.Environment().(*kube.Environment).Settings().LoadBalancerSupported {
					t.Skip("ingress status not supported without load balancer")
				}

				ip := apps.Ingress.HTTPAddress().IP.String()
				retry.UntilSuccessOrFail(ctx, func() error {
					ing, err := ctx.Clusters().Default().NetworkingV1beta1().Ingresses(apps.Namespace.Name()).Get(context.Background(), "ingress", metav1.GetOptions{})
					if err != nil {
						return err
					}
					if len(ing.Status.LoadBalancer.Ingress) != 1 || ing.Status.LoadBalancer.Ingress[0].IP != ip {
						return fmt.Errorf("unexpected ingress status, got %+v want %v", ing.Status.LoadBalancer, ip)
					}
					return nil
				}, retry.Delay(time.Second*5), retry.Timeout(time.Second*90))
			})

			// setup another ingress pointing to a different route; the ingress will have an ingress class that should be targeted at first
			const updateIngressName = "update-test-ingress"
			if err := ctx.Config().ApplyYAML(apps.Namespace.Name(), ingressClassConfig,
				fmt.Sprintf(ingressConfigTemplate, updateIngressName, "istio-test", "/update-test", "/update-test")); err != nil {
				t.Fatal(err)
			}
			// these cases make sure that when new Ingress configs are applied our controller picks up on them
			// and updates the accessible ingress-gateway routes accordingly
			ingressUpdateCases := []struct {
				name         string
				ingressClass string
				path         string
				call         echo.CallOptions
			}{
				{
					name:         "update-class-not-istio",
					ingressClass: "not-istio",
					path:         "/update-test",
					call: echo.CallOptions{
						Port: &echo.Port{
							Protocol: protocol.HTTP,
						},
						Path: "/update-test",
						Headers: map[string][]string{
							"Host": {"server"},
						},
						Validator: echo.ExpectCode("404"),
					},
				},
				{
					name:         "update-class-istio",
					ingressClass: "istio-test",
					path:         "/update-test",
					call: echo.CallOptions{
						Port: &echo.Port{
							Protocol: protocol.HTTP,
						},
						Path: "/update-test",
						Headers: map[string][]string{
							"Host": {"server"},
						},
						Validator: echo.ExpectCode("200"),
					},
				},
				{
					name:         "update-path",
					ingressClass: "istio-test",
					path:         "/updated",
					call: echo.CallOptions{
						Port: &echo.Port{
							Protocol: protocol.HTTP,
						},
						Path: "/updated",
						Headers: map[string][]string{
							"Host": {"server"},
						},
						Validator: echo.ExpectCode("200"),
					},
				},
			}

			for _, c := range ingressUpdateCases {
				c := c
				updatedIngress := fmt.Sprintf(ingressConfigTemplate, updateIngressName, c.ingressClass, c.path, c.path)
				ctx.Config().ApplyYAMLOrFail(ctx, apps.Namespace.Name(), updatedIngress)
				ctx.NewSubTest(c.name).Run(func(ctx framework.TestContext) {
					apps.Ingress.CallEchoWithRetryOrFail(ctx, c.call, retry.Timeout(time.Minute))
				})
			}
		})
}
